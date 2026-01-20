package functions

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"backend-server/internal/domain/tools"
	"backend-server/internal/domain/tools/eino_integration"
	"backend-server/internal/domain/tools/types"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/shared/ip_geo"

	"github.com/cloudwego/eino/schema"
)

const (
	GetIPLocationFunctionName = "get_ip_location"
)

// GetIPLocationFunctionDesc defines the function description for LLM
var GetIPLocationFunctionDesc = map[string]interface{}{
	"type": "function",
	"function": map[string]interface{}{
		"name":        GetIPLocationFunctionName,
		"description": "查询当前IP地址所在地理位置信息，包括国家、省份、城市等详细信息",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"ip": map[string]interface{}{
					"type":        "string",
					"description": "要查询的IP地址，如果不提供则查询当前连接的IP地址",
				},
				"format": map[string]interface{}{
					"type":        "string",
					"description": "返回格式，可选值：'detailed'(详细)、'simple'(简单)",
					"default":     "detailed",
				},
			},
			"required": []string{},
		},
	},
}

// GetIPLocationFunction implements the IP location query function
type GetIPLocationFunction struct {
	config map[string]interface{}
}

// NewGetIPLocationFunction creates a new IP location function
func NewGetIPLocationFunction(args map[string]interface{}) *GetIPLocationFunction {
	return &GetIPLocationFunction{
		config: args,
	}
}

// Execute implements the function execution
func (f *GetIPLocationFunction) Execute(ctx context.Context, conn types.Connection, args map[string]interface{}) (*types.ActionResponse, error) {
	var logger *slog.Logger
	if conn != nil {
		logger = conn.GetLogger()
	}

	// Only add new keys, don't overwrite existing ones
	for key, value := range f.config {
		if _, exists := args[key]; !exists {
			args[key] = value
		}
	}

	// Extract parameters
	ip, _ := args["ip"].(string)
	format, _ := args["format"].(string)
	if format == "" {
		format = "detailed"
	}

	// Get client IP if not provided
	if ip == "" {
		if conn != nil {
			// 使用GetRemoteAddr方法获取IP（现在已支持真实IP获取）
			remoteAddr := conn.GetRemoteAddr()
			if remoteAddr != "" {
				// 如果返回的是ip:port格式，需要提取IP部分
				clientIP, _, err := net.SplitHostPort(remoteAddr)
				if err != nil {
					// 如果不是ip:port格式，直接使用
					ip = remoteAddr
				} else {
					ip = clientIP
				}
			} else {
				if logger != nil {
					logger.Error("无法获取客户端IP地址")
				}
				return types.NewActionResponse(types.ActionReqLLM, nil, "抱歉，无法获取客户端IP地址"), nil
			}
		} else {
			return types.NewActionResponse(types.ActionReqLLM, nil, "抱歉，无法获取IP地址信息"), nil
		}
	}

	if logger != nil {
		logger.Info("查询IP地理位置", "ip", ip, "format", format)
	}

	// 如果是局域网ip，则获取公网ip
	if isPrivateIP(ip) {
		if logger != nil {
			logger.Info("检测到局域网IP，尝试获取公网IP", "private_ip", ip)
		}

		publicIP, err := getPublicIP()
		if err != nil {
			if logger != nil {
				logger.Warn("获取公网IP失败，使用原IP", "error", err, "original_ip", ip)
			}
		} else {
			if logger != nil {
				logger.Info("成功获取公网IP", "public_ip", publicIP, "original_ip", ip)
			}
			ip = publicIP
		}
	}

	// Get IP location information
	geo, err := ip_geo.IPGeoGet(ip)
	if err != nil {
		if logger != nil {
			logger.Error("获取IP地理位置失败", "error", err)
		}
		log.Errorf("获取IP[%s]地理位置失败: %v", ip, err)
		return types.NewActionResponse(types.ActionReqLLM, "抱歉，无法获取该IP地址的地理位置信息", nil), nil
	}

	response := f.formatSimpleLocation(ip, geo.Country, geo.Province, geo.City, geo.ISP)
	if logger != nil {
		logger.Info("IP地理位置查询成功", "ip", ip, "位置:", response)
	}

	return types.NewActionResponse(types.ActionReqLLM, nil, response), nil
}

// formatSimpleLocation formats simple location information
func (f *GetIPLocationFunction) formatSimpleLocation(ip, country, province, city, isp string) string {
	var response strings.Builder

	response.WriteString(fmt.Sprintf("IP地址 %s 的位置信息：\n\n", ip))

	if country != "0" && country != "" {
		response.WriteString(fmt.Sprintf("国家：%s\n", country))
	}

	if province != "0" && province != "" {
		response.WriteString(fmt.Sprintf("省份：%s\n", province))
	}

	if city != "0" && city != "" {
		response.WriteString(fmt.Sprintf("城市：%s\n", city))
	}

	if isp != "0" && isp != "" {
		response.WriteString(fmt.Sprintf("网络服务商：%s\n", isp))
	}

	return response.String()
}

// GetInfo returns the eino ToolInfo
func (f *GetIPLocationFunction) GetInfo() *schema.ToolInfo {
	converter := eino_integration.GetGlobalConverter()
	toolInfo, err := converter.ConvertPythonDescToEinoToolInfo(GetIPLocationFunctionName, GetIPLocationFunctionDesc)
	if err != nil {
		log.Errorf("Failed to convert tool info for %s: %v", GetIPLocationFunctionName, err)
		return nil
	}
	return toolInfo
}

// GetType returns the tool type
func (f *GetIPLocationFunction) GetType() types.ToolType {
	return types.ToolTypeWait
}

// GetName returns the function name
func (f *GetIPLocationFunction) GetName() string {
	return GetIPLocationFunctionName
}

// GetDescription returns the function description
func (f *GetIPLocationFunction) GetDescription() interface{} {
	return GetIPLocationFunctionDesc
}

// RegisterGetIPLocationFunction registers the IP location function
func RegisterGetIPLocationFunction() error {
	function := NewGetIPLocationFunction(nil)
	return tools.RegisterGlobalFunction(GetIPLocationFunctionName, function)
}

// init automatically registers the function when the package is imported
func init() {
	if err := RegisterGetIPLocationFunction(); err != nil {
		log.Errorf("Failed to register %s function: %v", GetIPLocationFunctionName, err)
	}
}

// isPrivateIP 判断是否为私有IP地址
func isPrivateIP(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	// 检查是否为私有IP地址范围
	// 10.0.0.0/8
	// 172.16.0.0/12
	// 192.168.0.0/16
	// 127.0.0.0/8 (localhost)
	// 169.254.0.0/16 (link-local)

	if parsedIP.IsLoopback() || parsedIP.IsLinkLocalUnicast() {
		return true
	}

	// 检查私有网络范围
	privateBlocks := []*net.IPNet{
		{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(8, 32)},
		{IP: net.IPv4(172, 16, 0, 0), Mask: net.CIDRMask(12, 32)},
		{IP: net.IPv4(192, 168, 0, 0), Mask: net.CIDRMask(16, 32)},
	}

	for _, block := range privateBlocks {
		if block.Contains(parsedIP) {
			return true
		}
	}

	return false
}

// getPublicIP 获取公网IP地址
func getPublicIP() (string, error) {
	// 使用多个IP查询服务，提高成功率
	// 优先使用中国国内服务，提高访问速度和稳定性
	services := []string{
		// 中国国内服务（优先使用，访问速度快）
		"https://ip.3322.net",              // 3322.net - 老牌IP查询服务，稳定可靠
		"https://myip.ipip.net",            // ipip.net - 提供详细地理位置信息
		"https://ip.chinaz.com/getip.aspx", // chinaz.com - 站长工具IP查询
		"https://ip.tool.lu",               // tool.lu - 在线工具IP查询
		"https://ip.qq.com/cgi-bin/query",  // qq.com - QQ IP查询服务
		// 国外备用服务（当国内服务不可用时使用）
		"https://api.ipify.org",      // ipify.org - 国外知名IP查询服务
		"https://ipv4.icanhazip.com", // icanhazip.com - 简单IP查询
		"https://ident.me",           // ident.me - 轻量级IP查询
		"https://api.ip.sb/ip",       // ip.sb - 简洁的IP查询服务
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	for _, service := range services {
		ip, err := getIPFromService(client, service)
		if err == nil && ip != "" {
			return ip, nil
		}
	}

	return "", fmt.Errorf("无法从任何服务获取公网IP")
}

// getIPFromService 从指定服务获取IP地址
func getIPFromService(client *http.Client, serviceURL string) (string, error) {
	req, err := http.NewRequest("GET", serviceURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	responseText := strings.TrimSpace(string(body))

	// 根据不同服务处理返回格式
	ip := extractIPFromResponse(serviceURL, responseText)

	// 验证返回的是否为有效IP地址
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("无效的IP地址: %s", ip)
	}

	return ip, nil
}

// extractIPFromResponse 从不同服务的响应中提取IP地址
func extractIPFromResponse(serviceURL, responseText string) string {
	// 根据不同服务处理返回格式
	switch {
	case strings.Contains(serviceURL, "ip.3322.net"):
		// 3322.net 直接返回IP
		return responseText
	case strings.Contains(serviceURL, "myip.ipip.net"):
		// ipip.net 返回格式: "当前 IP：xxx.xxx.xxx.xxx  来自于：中国 北京 北京"
		lines := strings.Split(responseText, "\n")
		for _, line := range lines {
			if strings.Contains(line, "当前 IP：") {
				parts := strings.Split(line, "当前 IP：")
				if len(parts) > 1 {
					ipPart := strings.Split(parts[1], " ")[0]
					return strings.TrimSpace(ipPart)
				}
			}
		}
		return responseText
	case strings.Contains(serviceURL, "ip.chinaz.com"):
		// chinaz.com 返回格式: "xxx.xxx.xxx.xxx"
		return responseText
	case strings.Contains(serviceURL, "ip.tool.lu"):
		// tool.lu 返回格式: "xxx.xxx.xxx.xxx"
		return responseText
	case strings.Contains(serviceURL, "ip.qq.com"):
		// qq.com 返回格式: "xxx.xxx.xxx.xxx"
		return responseText
	case strings.Contains(serviceURL, "api.ip.sb"):
		// ip.sb 直接返回IP
		return responseText
	case strings.Contains(serviceURL, "api.ipify.org"):
		// ipify.org 直接返回IP
		return responseText
	case strings.Contains(serviceURL, "icanhazip.com"):
		// icanhazip.com 直接返回IP
		return responseText
	case strings.Contains(serviceURL, "ident.me"):
		// ident.me 直接返回IP
		return responseText
	default:
		// 其他服务直接返回IP
		return responseText
	}
}
