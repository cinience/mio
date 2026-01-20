package functions

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	manager_api "backend-server/internal/adapters/manager"
	manager_types "backend-server/internal/adapters/manager/types"
	"backend-server/internal/app/service"
	"backend-server/internal/config"
	"backend-server/internal/domain/meetingminutes"
	"backend-server/internal/domain/tools"
	"backend-server/internal/domain/tools/eino_integration"
	"backend-server/internal/domain/tools/types"
	log "backend-server/internal/infrastructure/logger"

	"github.com/cloudwego/eino/schema"
)

const (
	StartMeetingMinutesFunctionName = "start_meeting_minutes"
	StopMeetingMinutesFunctionName  = "stop_meeting_minutes"
)

func startMeetingMinutesDesc() map[string]interface{} {
	startPhrase := strings.TrimSpace(resolveMeetingMinutesConfig().StartPhrase)
	if startPhrase == "" {
		startPhrase = "开始会议纪要"
	}
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        StartMeetingMinutesFunctionName,
			"description": fmt.Sprintf("当用户明确说出“%s”等开始纪要指令时调用，开始会议纪要模式并记录后续ASR文本。", startPhrase),
			"parameters": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

func stopMeetingMinutesDesc() map[string]interface{} {
	stopPhrase := strings.TrimSpace(resolveMeetingMinutesConfig().StopPhrase)
	if stopPhrase == "" {
		stopPhrase = "结束会议纪要"
	}
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        StopMeetingMinutesFunctionName,
			"description": fmt.Sprintf("当用户明确说出“%s”等结束纪要指令时调用，结束会议纪要并生成Markdown纪要文件。", stopPhrase),
			"parameters": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

type StartMeetingMinutesFunction struct{}

func NewStartMeetingMinutesFunction() *StartMeetingMinutesFunction {
	return &StartMeetingMinutesFunction{}
}

func (f *StartMeetingMinutesFunction) Execute(ctx context.Context, conn types.Connection, _ map[string]interface{}) (*types.ActionResponse, error) {
	cfg := resolveMeetingMinutesConfig()
	if !cfg.Enabled {
		return types.NewActionResponse(types.ActionDirectResponse, nil, "会议纪要未启用"), nil
	}
	var logger *slog.Logger
	if conn != nil {
		logger = conn.GetLogger()
	}
	if conn == nil {
		return types.NewActionResponse(types.ActionError, nil, "会话未就绪，无法开始会议纪要"), fmt.Errorf("connection is nil")
	}
	if err := conn.StartMeetingMinutes(); err != nil {
		if err == meetingminutes.ErrAlreadyActive {
			return types.NewActionResponse(types.ActionDirectResponse, nil, "会议纪要已在进行中"), nil
		}
		return types.NewActionResponse(types.ActionError, nil, "开始会议纪要失败"), err
	}
	if logger != nil {
		logger.Info("会议纪要已开始", "device_id", conn.GetDeviceID())
	}
	return types.NewActionResponse(types.ActionDirectResponse, nil, "已开始记录会议纪要"), nil
}

func (f *StartMeetingMinutesFunction) GetInfo() *schema.ToolInfo {
	converter := eino_integration.GetGlobalConverter()
	toolInfo, err := converter.ConvertPythonDescToEinoToolInfo(StartMeetingMinutesFunctionName, startMeetingMinutesDesc())
	if err != nil {
		log.Errorf("Failed to convert tool info for %s: %v", StartMeetingMinutesFunctionName, err)
		return nil
	}
	return toolInfo
}

func (f *StartMeetingMinutesFunction) GetType() types.ToolType {
	return types.ToolTypeSystemCtl
}

func (f *StartMeetingMinutesFunction) GetName() string {
	return StartMeetingMinutesFunctionName
}

func (f *StartMeetingMinutesFunction) GetDescription() interface{} {
	return startMeetingMinutesDesc()
}

func RegisterStartMeetingMinutesFunction() error {
	function := NewStartMeetingMinutesFunction()
	return tools.RegisterGlobalFunction(StartMeetingMinutesFunctionName, function)
}

type StopMeetingMinutesFunction struct{}

func NewStopMeetingMinutesFunction() *StopMeetingMinutesFunction {
	return &StopMeetingMinutesFunction{}
}

func (f *StopMeetingMinutesFunction) Execute(ctx context.Context, conn types.Connection, _ map[string]interface{}) (*types.ActionResponse, error) {
	cfg := resolveMeetingMinutesConfig()
	if !cfg.Enabled {
		return types.NewActionResponse(types.ActionDirectResponse, nil, "会议纪要未启用"), nil
	}
	var logger *slog.Logger
	deviceID := ""
	if conn != nil {
		logger = conn.GetLogger()
		deviceID = conn.GetDeviceID()
	}
	if conn == nil {
		return types.NewActionResponse(types.ActionError, nil, "会话未就绪，无法结束会议纪要"), fmt.Errorf("connection is nil")
	}
	snapshot, err := conn.StopMeetingMinutes()
	if err != nil {
		if err == meetingminutes.ErrNotActive {
			return types.NewActionResponse(types.ActionDirectResponse, nil, "当前未开启会议纪要"), nil
		}
		return types.NewActionResponse(types.ActionError, nil, "结束会议纪要失败"), err
	}
	if snapshot == nil {
		return types.NewActionResponse(types.ActionError, nil, "会议纪要内容为空"), fmt.Errorf("meeting minutes snapshot is nil")
	}

	persisted, err := meetingminutes.PersistSnapshot(*snapshot, cfg.OutputDir, timeNow())
	if err != nil {
		log.Errorf("写入会议纪要失败: %v", err)
		return types.NewActionResponse(types.ActionError, nil, "生成会议纪要失败"), err
	}

	meta := persisted.Metadata
	appCfg := config.GetConfig()
	result, uploadErr := ingestMeetingMinutes(ctx, appCfg, conn, persisted)
	if uploadErr != nil {
		meta.Status = meetingminutes.StatusFailed
		meta.Error = uploadErr.Error()
	} else {
		meta.Status = meetingminutes.StatusSucceeded
		if result != nil {
			meta.KnowledgeBaseID = result.KnowledgeBaseID
			meta.DocumentID = result.DocumentID
			meta.JobID = result.JobID
		}
	}
	if updateErr := meetingminutes.FinalizeIngestion(persisted.MetadataPath, meta); updateErr != nil {
		log.Errorf("更新会议纪要元数据失败: %v", updateErr)
	}

	if logger != nil {
		logger.Info("会议纪要已生成", "device_id", deviceID, "file", persisted.FilePath, "ingest_error", uploadErr)
	}

	if uploadErr != nil {
		return types.NewActionResponse(types.ActionDirectResponse, nil, "会议纪要已生成，但入库失败"), nil
	}
	return types.NewActionResponse(types.ActionDirectResponse, nil, "会议纪要已生成并入库"), nil
}

func (f *StopMeetingMinutesFunction) GetInfo() *schema.ToolInfo {
	converter := eino_integration.GetGlobalConverter()
	toolInfo, err := converter.ConvertPythonDescToEinoToolInfo(StopMeetingMinutesFunctionName, stopMeetingMinutesDesc())
	if err != nil {
		log.Errorf("Failed to convert tool info for %s: %v", StopMeetingMinutesFunctionName, err)
		return nil
	}
	return toolInfo
}

func (f *StopMeetingMinutesFunction) GetType() types.ToolType {
	return types.ToolTypeSystemCtl
}

func (f *StopMeetingMinutesFunction) GetName() string {
	return StopMeetingMinutesFunctionName
}

func (f *StopMeetingMinutesFunction) GetDescription() interface{} {
	return stopMeetingMinutesDesc()
}

func RegisterStopMeetingMinutesFunction() error {
	function := NewStopMeetingMinutesFunction()
	return tools.RegisterGlobalFunction(StopMeetingMinutesFunctionName, function)
}

func init() {
	if err := RegisterStartMeetingMinutesFunction(); err != nil {
		log.Errorf("Failed to register %s function: %v", StartMeetingMinutesFunctionName, err)
	}
	if err := RegisterStopMeetingMinutesFunction(); err != nil {
		log.Errorf("Failed to register %s function: %v", StopMeetingMinutesFunctionName, err)
	}
}

func ingestMeetingMinutes(ctx context.Context, cfg *config.AppConfig, conn types.Connection, persisted *meetingminutes.PersistedMinutes) (*manager_types.MeetingMinutesUploadResult, error) {
	if persisted == nil {
		return nil, fmt.Errorf("meeting minutes file is nil")
	}
	managerSvc := managerServiceFromConn(conn)
	if managerSvc == nil {
		return nil, fmt.Errorf("manager api unavailable")
	}
	file, err := os.Open(persisted.FilePath)
	if err != nil {
		return nil, fmt.Errorf("open meeting minutes: %w", err)
	}
	defer file.Close()

	deviceID := ""
	sessionID := persisted.Metadata.SessionID
	if conn != nil {
		deviceID = conn.GetDeviceID()
	}
	if cfg == nil {
		return nil, fmt.Errorf("config unavailable")
	}
	kbID := cfg.MeetingMinutes.KBID
	req := &manager_types.MeetingMinutesUploadRequest{
		DeviceID:        deviceID,
		SessionID:       sessionID,
		KnowledgeBaseID: kbID,
		FileName:        filepath.Base(persisted.FilePath),
		ContentType:     "text/markdown",
		Reader:          file,
	}
	return managerSvc.UploadMeetingMinutes(ctx, req)
}

func managerServiceFromConn(conn types.Connection) manager_api.ManagerAPIService {
	if conn != nil {
		if svc := conn.GetManagerAPIService(); svc != nil {
			return svc
		}
	}
	return service.DefaultRegistry().ManagerAPIService()
}

func timeNow() time.Time {
	return time.Now()
}

func resolveMeetingMinutesConfig() config.MeetingMinutesConfig {
	cfg := config.GetConfig()
	if cfg == nil {
		return config.MeetingMinutesConfig{
			Enabled:     true,
			StartPhrase: "开始会议纪要",
			StopPhrase:  "结束会议纪要",
			OutputDir:   "./data/meeting-minutes",
		}
	}
	return cfg.MeetingMinutes
}
