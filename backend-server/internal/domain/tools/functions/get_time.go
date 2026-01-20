package functions

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"backend-server/internal/domain/tools"
	"backend-server/internal/domain/tools/eino_integration"
	"backend-server/internal/domain/tools/types"
	"backend-server/internal/domain/tools/utils"
	log "backend-server/internal/infrastructure/logger"

	"github.com/cloudwego/eino/schema"
)

const (
	GetTimeFunctionName = "get_time"
)

// GetTimeFunctionDesc defines the function description for LLM
var GetTimeFunctionDesc = map[string]interface{}{
	"type": "function",
	"function": map[string]interface{}{
		"name":        GetTimeFunctionName,
		"description": "获取当前时间信息，包括公历时间、农历时间、节气、生肖等详细信息",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"timezone": map[string]interface{}{
					"type":        "string",
					"description": "时区，如'Asia/Shanghai'、'UTC'等，默认为本地时区",
					"default":     "Asia/Shanghai",
				},
				"format": map[string]interface{}{
					"type":        "string",
					"description": "返回格式，可选值：'detailed'(详细)、'simple'(简单)、'lunar'(农历重点)",
					"default":     "detailed",
				},
				"include_solar_term": map[string]interface{}{
					"type":        "boolean",
					"description": "是否包含节气信息",
					"default":     true,
				},
			},
			"required": []string{},
		},
	},
}

// GetTimeFunction implements the time query function
type GetTimeFunction struct{}

// NewGetTimeFunction creates a new time function
func NewGetTimeFunction(args map[string]interface{}) *GetTimeFunction {
	return &GetTimeFunction{}
}

// Execute implements the function execution
func (f *GetTimeFunction) Execute(ctx context.Context, conn types.Connection, args map[string]interface{}) (*types.ActionResponse, error) {
	var logger *slog.Logger
	if conn != nil {
		logger = conn.GetLogger()
	}

	// Extract parameters
	timezone, _ := args["timezone"].(string)
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}

	format, _ := args["format"].(string)
	if format == "" {
		format = "detailed"
	}

	includeSolarTerm := true
	if include, ok := args["include_solar_term"].(bool); ok {
		includeSolarTerm = include
	}

	if logger != nil {
		logger.Info("查询时间信息", "timezone", timezone, "format", format, "include_solar_term", includeSolarTerm)
	}

	// Try to get time info from cache first
	cacheKey := fmt.Sprintf("time_%s_%s_%v", timezone, format, includeSolarTerm)
	if cachedData, found := utils.GetCache(utils.CacheTypeLunar, cacheKey); found {
		if logger != nil {
			logger.Debug("从缓存获取时间数据")
		}
		return types.NewActionResponse(types.ActionDirectResponse, cachedData, nil), nil
	}

	// Get current time in specified timezone
	currentTime, err := f.getCurrentTime(timezone)
	if err != nil {
		if logger != nil {
			logger.Error("获取时间失败", "error", err)
		}
		return types.NewActionResponse(types.ActionDirectResponse, "抱歉，获取时间信息失败", nil), nil
	}

	// Get comprehensive time information
	timeInfo := utils.GetTimeInfo(currentTime)

	// Format response based on requested format
	response := f.formatTimeResponse(timeInfo, format, includeSolarTerm, currentTime)

	// Cache the response for a short time (1 minute)
	utils.SetCache(utils.CacheTypeLunar, cacheKey, response, 1*time.Minute)

	if logger != nil {
		logger.Info("时间查询成功", "timezone", timezone)
	}

	return types.NewActionResponse(types.ActionDirectResponse, response, nil), nil
}

// getCurrentTime gets current time in the specified timezone
func (f *GetTimeFunction) getCurrentTime(timezone string) (time.Time, error) {
	// Load timezone
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		// Fallback to local time if timezone loading fails
		return time.Now(), nil
	}

	return time.Now().In(loc), nil
}

// formatTimeResponse formats the time information based on the requested format
func (f *GetTimeFunction) formatTimeResponse(timeInfo map[string]interface{}, format string, includeSolarTerm bool, currentTime time.Time) string {
	var response strings.Builder

	switch format {
	case "simple":
		response.WriteString(f.formatSimpleTime(timeInfo, currentTime))
	case "lunar":
		response.WriteString(f.formatLunarTime(timeInfo, includeSolarTerm))
	case "detailed":
		fallthrough
	default:
		response.WriteString(f.formatDetailedTime(timeInfo, includeSolarTerm, currentTime))
	}

	return response.String()
}

// formatSimpleTime formats simple time information
func (f *GetTimeFunction) formatSimpleTime(timeInfo map[string]interface{}, currentTime time.Time) string {
	var response strings.Builder

	response.WriteString(fmt.Sprintf("🕐 当前时间：%s %s\n",
		timeInfo["solar_date"], timeInfo["time_string"]))
	response.WriteString(fmt.Sprintf("📅 %s\n", timeInfo["solar_weekday"]))
	response.WriteString(fmt.Sprintf("🏮 农历：%s\n", timeInfo["lunar_date"]))

	if festival, ok := timeInfo["festival"].(string); ok && festival != "" {
		response.WriteString(fmt.Sprintf("🎉 节日：%s\n", festival))
	}

	return response.String()
}

// formatLunarTime formats lunar-focused time information
func (f *GetTimeFunction) formatLunarTime(timeInfo map[string]interface{}, includeSolarTerm bool) string {
	var response strings.Builder

	response.WriteString(fmt.Sprintf("🏮 农历时间信息：\n\n"))
	response.WriteString(fmt.Sprintf("📅 农历日期：%s\n", timeInfo["lunar_date"]))
	response.WriteString(fmt.Sprintf("🐲 生肖年：%s年\n", timeInfo["zodiac"]))
	response.WriteString(fmt.Sprintf("📜 干支纪年：%s\n", timeInfo["year_name"]))

	if festival, ok := timeInfo["festival"].(string); ok && festival != "" {
		response.WriteString(fmt.Sprintf("🎉 传统节日：%s\n", festival))
	}

	if includeSolarTerm {
		response.WriteString(fmt.Sprintf("🌱 当前节气：%s\n", timeInfo["solar_term"]))
	}

	response.WriteString(fmt.Sprintf("\n📍 对应公历：%s %s",
		timeInfo["solar_date"], timeInfo["solar_weekday"]))

	return response.String()
}

// formatDetailedTime formats detailed time information
func (f *GetTimeFunction) formatDetailedTime(timeInfo map[string]interface{}, includeSolarTerm bool, currentTime time.Time) string {
	var response strings.Builder

	response.WriteString("🕐 详细时间信息：\n\n")

	// Solar calendar information
	response.WriteString("📅 **公历信息**\n")
	response.WriteString(fmt.Sprintf("   日期：%s\n", timeInfo["solar_date"]))
	response.WriteString(fmt.Sprintf("   时间：%s\n", timeInfo["time_string"]))
	response.WriteString(fmt.Sprintf("   星期：%s\n", timeInfo["solar_weekday"]))
	response.WriteString(fmt.Sprintf("   时区：%s\n", currentTime.Location().String()))

	response.WriteString("\n🏮 **农历信息**\n")
	response.WriteString(fmt.Sprintf("   农历：%s\n", timeInfo["lunar_date"]))
	response.WriteString(fmt.Sprintf("   干支：%s\n", timeInfo["year_name"]))
	response.WriteString(fmt.Sprintf("   生肖：%s年\n", timeInfo["zodiac"]))

	if festival, ok := timeInfo["festival"].(string); ok && festival != "" {
		response.WriteString(fmt.Sprintf("   节日：%s\n", festival))
	}

	if includeSolarTerm {
		response.WriteString(fmt.Sprintf("\n🌱 **节气信息**\n"))
		response.WriteString(fmt.Sprintf("   当前节气：%s\n", timeInfo["solar_term"]))
	}

	// Additional information
	response.WriteString(fmt.Sprintf("\n⏰ **其他信息**\n"))
	response.WriteString(fmt.Sprintf("   Unix时间戳：%d\n", currentTime.Unix()))
	response.WriteString(fmt.Sprintf("   年份：%d年\n", currentTime.Year()))
	response.WriteString(fmt.Sprintf("   第%d周\n", f.getWeekOfYear(currentTime)))
	response.WriteString(fmt.Sprintf("   第%d天\n", currentTime.YearDay()))

	return response.String()
}

// getWeekOfYear returns the week number of the year
func (f *GetTimeFunction) getWeekOfYear(t time.Time) int {
	_, week := t.ISOWeek()
	return week
}

// GetInfo returns the eino ToolInfo
func (f *GetTimeFunction) GetInfo() *schema.ToolInfo {
	converter := eino_integration.GetGlobalConverter()
	toolInfo, err := converter.ConvertPythonDescToEinoToolInfo(GetTimeFunctionName, GetTimeFunctionDesc)
	if err != nil {
		log.Errorf("Failed to convert tool info for %s: %v", GetTimeFunctionName, err)
		return nil
	}
	return toolInfo
}

// GetType returns the tool type
func (f *GetTimeFunction) GetType() types.ToolType {
	return types.ToolTypeWait
}

// GetName returns the function name
func (f *GetTimeFunction) GetName() string {
	return GetTimeFunctionName
}

// GetDescription returns the function description
func (f *GetTimeFunction) GetDescription() interface{} {
	return GetTimeFunctionDesc
}

// GetSupportedTimezones returns a list of commonly used timezones
func (f *GetTimeFunction) GetSupportedTimezones() []string {
	return []string{
		"Asia/Shanghai",       // 中国标准时间
		"Asia/Hong_Kong",      // 香港时间
		"Asia/Taipei",         // 台北时间
		"Asia/Tokyo",          // 东京时间
		"Asia/Seoul",          // 首尔时间
		"UTC",                 // 协调世界时
		"America/New_York",    // 纽约时间
		"America/Los_Angeles", // 洛杉矶时间
		"Europe/London",       // 伦敦时间
		"Europe/Paris",        // 巴黎时间
	}
}

// GetSupportedFormats returns a list of supported formats
func (f *GetTimeFunction) GetSupportedFormats() []string {
	return []string{
		"detailed", // 详细格式
		"simple",   // 简单格式
		"lunar",    // 农历重点格式
	}
}

// RegisterGetTimeFunction registers the time function
func RegisterGetTimeFunction() error {
	function := NewGetTimeFunction(nil)
	return tools.RegisterGlobalFunction(GetTimeFunctionName, function)
}

// init automatically registers the function when the package is imported
func init() {
	if err := RegisterGetTimeFunction(); err != nil {
		log.Errorf("Failed to register %s function: %v", GetTimeFunctionName, err)
	}
}
