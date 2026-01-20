package api

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"manager-server/internal/models"
	"manager-server/internal/service"
)

// 简易内存映射替代Redis: uuid -> otaId，及下载次数限制
var otaDownloadStore = struct {
	mu       sync.Mutex
	uuidToID map[string]string
	dlCount  map[string]int
}{uuidToID: map[string]string{}, dlCount: map[string]int{}}

type OTAMagHandlers struct {
	service service.OtaService
}

func NewOTAMagHandlers(service service.OtaService) *OTAMagHandlers {
	return &OTAMagHandlers{service: service}
}

// page 分页查询 OTA 固件信息
func (h *OTAMagHandlers) page(c *gin.Context) {
	pageStr := c.Query("page")
	limitStr := c.Query("limit")
	var req models.OtaPageReqDTO
	if pageStr != "" {
		if v, err := strconv.Atoi(pageStr); err == nil {
			req.PageNum = v
		}
	}
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil {
			req.PageSize = v
		}
	}
	resp, err := h.service.Page(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, &models.CommonResponse{Code: 0, Msg: "success", Data: map[string]interface{}{
		"total": resp.TotalCount,
		"list":  resp.List,
	}})
}

// get 获取OTA固件信息
func (h *OTAMagHandlers) get(c *gin.Context) {
	id := c.Param("id")
	item, err := h.service.GetByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, &models.CommonResponse{Code: 0, Msg: "success", Data: item})
}

// save 保存OTA固件信息
func (h *OTAMagHandlers) save(c *gin.Context) {
	var entity models.OtaEntity
	if err := c.ShouldBindJSON(&entity); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: "参数错误: " + err.Error()})
		return
	}
	if err := h.service.Save(c.Request.Context(), &entity); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, &models.CommonResponse{Code: 0, Msg: "success"})
}

// delete 删除OTA固件
func (h *OTAMagHandlers) delete(c *gin.Context) {
	idStr := c.Param("id")
	ids := strings.Split(idStr, ",")
	if err := h.service.Delete(c.Request.Context(), ids); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, &models.CommonResponse{Code: 0, Msg: "success"})
}

// update 更新OTA固件
func (h *OTAMagHandlers) update(c *gin.Context) {
	id := c.Param("id")
	var entity models.OtaEntity
	if err := c.ShouldBindJSON(&entity); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: "参数错误: " + err.Error()})
		return
	}
	entity.ID = id
	if err := h.service.Update(c.Request.Context(), &entity); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, &models.CommonResponse{Code: 0, Msg: "success"})
}

// getDownloadUrl 获取下载uuid
func (h *OTAMagHandlers) getDownloadUrl(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: "id不能为空"})
		return
	}
	u := uuid.NewString()
	otaDownloadStore.mu.Lock()
	otaDownloadStore.uuidToID[u] = id
	otaDownloadStore.dlCount[u] = 0
	otaDownloadStore.mu.Unlock()
	c.JSON(http.StatusOK, &models.CommonResponse{Code: 0, Msg: "success", Data: u})
}

// download 提供固件下载
func (h *OTAMagHandlers) download(c *gin.Context) {
	u := c.Param("uuid")
	if u == "" {
		c.Status(http.StatusNotFound)
		return
	}
	otaDownloadStore.mu.Lock()
	id, ok := otaDownloadStore.uuidToID[u]
	count := otaDownloadStore.dlCount[u]
	if !ok {
		otaDownloadStore.mu.Unlock()
		c.Status(http.StatusNotFound)
		return
	}
	if count >= 3 {
		delete(otaDownloadStore.uuidToID, u)
		delete(otaDownloadStore.dlCount, u)
		otaDownloadStore.mu.Unlock()
		c.Status(http.StatusNotFound)
		return
	}
	otaDownloadStore.dlCount[u] = count + 1
	otaDownloadStore.mu.Unlock()

	item, err := h.service.GetByID(c.Request.Context(), id)
	if err != nil || item == nil || item.FirmwarePath == "" {
		c.Status(http.StatusNotFound)
		return
	}

	path := item.FirmwarePath
	// 解析绝对或相对路径
	resolved := path
	if !filepath.IsAbs(resolved) {
		cwd, _ := os.Getwd()
		resolved = filepath.Join(cwd, resolved)
	}
	// 若不存在，尝试 firmware/ 文件夹
	if _, err := os.Stat(resolved); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			base := filepath.Base(path)
			cwd, _ := os.Getwd()
			alt := filepath.Join(cwd, "firmware", base)
			if _, err2 := os.Stat(alt); err2 == nil {
				resolved = alt
			} else {
				c.Status(http.StatusNotFound)
				return
			}
		} else {
			c.Status(http.StatusInternalServerError)
			return
		}
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	filename := item.Type + "_" + item.Version
	if ext := filepath.Ext(path); ext != "" {
		filename += ext
	}
	// 安全化文件名
	filename = safeFilename(filename)
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Data(http.StatusOK, "application/octet-stream", data)
}

func safeFilename(name string) string {
	b := strings.Builder{}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
