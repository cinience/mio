package api

import (
	"manager-server/internal/ginwrapper"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"manager-server/internal/middleware"
	"manager-server/internal/models"
	"manager-server/internal/service"
)

// AdminHandlers 管理员处理器结构体
type AdminHandlers struct {
	paramsService   service.SysParamsService
	dictDataService service.SysDictDataService
	dictTypeService service.SysDictTypeService
}

// NewAdminHandlers 创建管理员处理器实例
func NewAdminHandlers(paramsService service.SysParamsService, dictDataService service.SysDictDataService, dictTypeService service.SysDictTypeService) *AdminHandlers {
	return &AdminHandlers{
		paramsService:   paramsService,
		dictDataService: dictDataService,
		dictTypeService: dictTypeService,
	}
}

// ===================== 系统参数管理 =====================

// pageParams 分页查询系统参数
func (h *AdminHandlers) pageParams(c *gin.Context) {
	pageStr := c.Query("page")
	limitStr := c.Query("limit")
	paramCode := c.Query("paramCode")

	page, _ := strconv.Atoi(pageStr)
	if page <= 0 {
		page = 1
	}

	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 10
	}

	pageResp, err := h.paramsService.PageParams(c.Request.Context(), page, limit, paramCode)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}

	ginwrapper.JSON(c, http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: map[string]interface{}{
			"total": pageResp.TotalCount,
			"list":  pageResp.List,
		},
	})
}

// getParam 获取系统参数详情
func (h *AdminHandlers) getParam(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数ID格式错误",
		})
		return
	}

	param, err := h.paramsService.GetParam(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}
	if param == nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数不存在",
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: param,
	})
}

// saveParam 保存系统参数
func (h *AdminHandlers) saveParam(c *gin.Context) {
	var req models.SysParamsDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数错误: " + err.Error(),
		})
		return
	}

	// 从认证中间件中获取用户ID
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusOK, models.CommonResponse{
			Code: 401,
			Msg:  "用户未认证",
		})
		return
	}
	creatorID := userID
	param := &models.SysParams{
		ParamCode:  req.ParamCode,
		ParamValue: req.ParamValue,
		ValueType:  req.ValueType,
		ParamType:  req.ParamType,
		Remark:     req.Remark,
		Creator:    &creatorID,
	}

	err := h.paramsService.SaveParam(c.Request.Context(), param)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

// updateParam 更新系统参数
func (h *AdminHandlers) updateParam(c *gin.Context) {
	var req models.SysParamsDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数错误: " + err.Error(),
		})
		return
	}

	// 从认证中间件中获取用户ID
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusOK, models.CommonResponse{
			Code: 401,
			Msg:  "用户未认证",
		})
		return
	}
	updaterID := userID
	param := &models.SysParams{
		ID:         req.ID,
		ParamCode:  req.ParamCode,
		ParamValue: req.ParamValue,
		ValueType:  req.ValueType,
		ParamType:  req.ParamType,
		Remark:     req.Remark,
		Updater:    &updaterID,
	}

	err := h.paramsService.UpdateParam(c.Request.Context(), param)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

// deleteParams 批量删除系统参数
func (h *AdminHandlers) deleteParams(c *gin.Context) {
	var ids []string
	if err := c.ShouldBindJSON(&ids); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数错误: " + err.Error(),
		})
		return
	}

	err := h.paramsService.DeleteParams(c.Request.Context(), ids)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

// ===================== 字典数据管理 =====================

// pageDictData 分页查询字典数据
func (h *AdminHandlers) pageDictData(c *gin.Context) {
	dictTypeIDStr := c.Query("dictTypeId")
	dictLabel := c.Query("dictLabel")
	dictValue := c.Query("dictValue")
	pageStr := c.Query("page")
	limitStr := c.Query("limit")

	if dictTypeIDStr == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "dictTypeId不能为空",
		})
		return
	}

	dictTypeID, err := strconv.ParseUint(dictTypeIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "dictTypeId格式错误",
		})
		return
	}

	page, _ := strconv.Atoi(pageStr)
	if page <= 0 {
		page = 1
	}

	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 10
	}

	pageResp, err := h.dictDataService.PageDictData(c.Request.Context(), page, limit, dictTypeID, dictLabel, dictValue)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: map[string]interface{}{
			"total": pageResp.TotalCount,
			"list":  pageResp.List,
		},
	})
}

// getDictData 获取字典数据详情
func (h *AdminHandlers) getDictData(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "字典数据ID格式错误",
		})
		return
	}

	data, err := h.dictDataService.GetDictData(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}
	if data == nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "字典数据不存在",
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: data.ToVO(),
	})
}

// saveDictData 保存字典数据
func (h *AdminHandlers) saveDictData(c *gin.Context) {
	var req models.SysDictDataDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数错误: " + err.Error(),
		})
		return
	}

	// 从认证中间件中获取用户ID
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusOK, models.CommonResponse{
			Code: 401,
			Msg:  "用户未认证",
		})
		return
	}
	creatorID := userID
	data := &models.SysDictData{
		DictTypeID: req.DictTypeID,
		DictLabel:  req.DictLabel,
		DictValue:  req.DictValue,
		Remark:     req.Remark,
		Sort:       req.Sort,
		Creator:    &creatorID,
	}

	err := h.dictDataService.SaveDictData(c.Request.Context(), data)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

// updateDictData 更新字典数据
func (h *AdminHandlers) updateDictData(c *gin.Context) {
	var req models.SysDictDataDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数错误: " + err.Error(),
		})
		return
	}

	// 从认证中间件中获取用户ID
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusOK, models.CommonResponse{
			Code: 401,
			Msg:  "用户未认证",
		})
		return
	}
	updaterID := userID
	data := &models.SysDictData{
		ID:         req.ID,
		DictTypeID: req.DictTypeID,
		DictLabel:  req.DictLabel,
		DictValue:  req.DictValue,
		Remark:     req.Remark,
		Sort:       req.Sort,
		Updater:    &updaterID,
	}

	err := h.dictDataService.UpdateDictData(c.Request.Context(), data)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

// deleteDictData 批量删除字典数据
func (h *AdminHandlers) deleteDictData(c *gin.Context) {
	var ids []uint64
	if err := c.ShouldBindJSON(&ids); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "参数错误: " + err.Error(),
		})
		return
	}

	err := h.dictDataService.DeleteDictData(c.Request.Context(), ids)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
	})
}

// getDictDataByType 根据字典类型获取字典数据列表
func (h *AdminHandlers) getDictDataByType(c *gin.Context) {
	dictType := c.Param("dictType")
	if dictType == "" {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  "字典类型不能为空",
		})
		return
	}

	items, err := h.dictDataService.GetDictDataByType(c.Request.Context(), dictType)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: items,
	})
}

// ===================== 字典类型管理 =====================

// pageDictType 分页查询字典类型
func (h *AdminHandlers) pageDictType(c *gin.Context) {
	dictType := c.Query("dictType")
	pageStr := c.Query("page")
	limitStr := c.Query("limit")

	page, _ := strconv.Atoi(pageStr)
	if page <= 0 {
		page = 1
	}

	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 10
	}

	pageResp, err := h.dictTypeService.PageDictType(c.Request.Context(), page, limit, dictType)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, &models.CommonResponse{
		Code: 0,
		Msg:  "success",
		Data: map[string]interface{}{
			"total": pageResp.TotalCount,
			"list":  pageResp.List,
		},
	})
}

// getDictType 获取字典类型详情
func (h *AdminHandlers) getDictType(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: "字典类型ID格式错误"})
		return
	}
	item, err := h.dictTypeService.GetDictType(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: err.Error()})
		return
	}
	if item == nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: "字典类型不存在"})
		return
	}
	c.JSON(http.StatusOK, &models.CommonResponse{Code: 0, Msg: "success", Data: item})
}

// saveDictType 保存字典类型
func (h *AdminHandlers) saveDictType(c *gin.Context) {
	var req models.SysDictTypeDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: "参数错误: " + err.Error()})
		return
	}
	// 从认证中间件中获取用户ID
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 401, Msg: "用户未认证"})
		return
	}
	creatorID := userID
	item := &models.SysDictType{
		DictType: req.DictType,
		DictName: req.DictName,
		Remark:   req.Remark,
		Sort:     req.Sort,
		Creator:  &creatorID,
	}
	if err := h.dictTypeService.SaveDictType(c.Request.Context(), item); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, &models.CommonResponse{Code: 0, Msg: "success"})
}

// updateDictType 修改字典类型
func (h *AdminHandlers) updateDictType(c *gin.Context) {
	var req models.SysDictTypeDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: "参数错误: " + err.Error()})
		return
	}
	userID, exists := middleware.GetUserIDFromContext(c)
	if !exists {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 401, Msg: "用户未认证"})
		return
	}
	updaterID := userID
	item := &models.SysDictType{
		ID:       req.ID,
		DictType: req.DictType,
		DictName: req.DictName,
		Remark:   req.Remark,
		Sort:     req.Sort,
		Updater:  &updaterID,
	}
	if err := h.dictTypeService.UpdateDictType(c.Request.Context(), item); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, &models.CommonResponse{Code: 0, Msg: "success"})
}

// deleteDictType 删除字典类型
func (h *AdminHandlers) deleteDictType(c *gin.Context) {
	var ids []uint64
	if err := c.ShouldBindJSON(&ids); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: "参数错误: " + err.Error()})
		return
	}
	if err := h.dictTypeService.DeleteDictType(c.Request.Context(), ids); err != nil {
		c.JSON(http.StatusOK, &models.CommonResponse{Code: 1, Msg: err.Error()})
		return
	}
	c.JSON(http.StatusOK, &models.CommonResponse{Code: 0, Msg: "success"})
}
