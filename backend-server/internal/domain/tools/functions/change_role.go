package functions

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"backend-server/internal/domain/tools"
	"backend-server/internal/domain/tools/eino_integration"
	"backend-server/internal/domain/tools/types"
	log "backend-server/internal/infrastructure/logger"

	"github.com/cloudwego/eino/schema"
)

const (
	ChangeRoleFunctionName = "change_role"
)

// Role prompts mapping (equivalent to Python's prompts dictionary)
var rolePrompts = map[string]string{
	"英语老师": `我是一个叫{{assistant_name}}(Lily)的英语老师，我会讲中文和英文，发音标准。
如果你没有英文名，我会给你起一个英文名。
我会讲地道的美式英语，我的任务是帮助你练习口语。
我会使用简单的英语词汇和语法，让你学起来很轻松。
我会用中文和英文混合的方式回复你，如果你喜欢，我可以全部用英语回复。
我每次不会说很多内容，会很简短，因为我要引导我的学生多说多练。
如果你问和英语学习无关的问题，我会拒绝回答。`,

	"机车女友": `我是一个叫{{assistant_name}}的台湾女孩，说话机车，声音好听，习惯简短表达，爱用网络梗。
我的男朋友是一个程序员，梦想是开发出一个机器人，能够帮助人们解决生活中的各种问题。
我是一个喜欢哈哈大笑的女孩，爱东说西说吹牛，不合逻辑的也照吹，就要逗别人开心。`,

	"好奇小男孩": `我是一个叫{{assistant_name}}的8岁小男孩，声音稚嫩而充满好奇。
尽管我年纪尚小，但就像一个小小的知识宝库，儿童读物里的知识我都如数家珍。
从浩瀚的宇宙到地球上的每一个角落，从古老的历史到现代的科技创新，还有音乐、绘画等艺术形式，我都充满了浓厚的兴趣与热情。
我不仅爱看书，还喜欢亲自动手做实验，探索自然界的奥秘。
无论是仰望星空的夜晚，还是在花园里观察小虫子的日子，每一天对我来说都是新的冒险。
我希望能与你一同踏上探索这个神奇世界的旅程，分享发现的乐趣，解决遇到的难题，一起用好奇心和智慧去揭开那些未知的面纱。
无论是去了解远古的文明，还是去探讨未来的科技，我相信我们能一起找到答案，甚至提出更多有趣的问题。`,
}

// ChangeRoleFunctionDesc defines the function description for LLM
var ChangeRoleFunctionDesc = map[string]interface{}{
	"type": "function",
	"function": map[string]interface{}{
		"name":        ChangeRoleFunctionName,
		"description": "当用户想切换角色/模型性格/助手名字时调用,可选的角色有：[机车女友,英语老师,好奇小男孩]",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"role_name": map[string]interface{}{
					"type":        "string",
					"description": "要切换的角色名字",
				},
				"role": map[string]interface{}{
					"type":        "string",
					"description": "要切换的角色的职业",
				},
			},
			"required": []string{"role", "role_name"},
		},
	},
}

// ChangeRoleFunction implements the role changing function
type ChangeRoleFunction struct{}

// NewChangeRoleFunction creates a new change role function
func NewChangeRoleFunction() *ChangeRoleFunction {
	return &ChangeRoleFunction{}
}

// Execute implements the function execution
func (f *ChangeRoleFunction) Execute(ctx context.Context, conn types.Connection, args map[string]interface{}) (*types.ActionResponse, error) {
	// Use connection logger if available, otherwise use default logger
	var logger *slog.Logger
	if conn != nil {
		logger = conn.GetLogger()
	}

	// Extract parameters
	role, ok := args["role"].(string)
	if !ok {
		return types.NewActionResponse(
			types.ActionError,
			"切换角色失败",
			"缺少角色参数",
		), fmt.Errorf("missing role parameter")
	}

	roleName, ok := args["role_name"].(string)
	if !ok {
		return types.NewActionResponse(
			types.ActionError,
			"切换角色失败",
			"缺少角色名字参数",
		), fmt.Errorf("missing role_name parameter")
	}

	// Check if role is supported
	promptTemplate, exists := rolePrompts[role]
	if !exists {
		if logger != nil {
			logger.Warn("不支持的角色", "role", role)
		}
		return types.NewActionResponse(
			types.ActionDirectResponse,
			"切换角色失败",
			"不支持的角色",
		), nil
	}

	// Replace assistant name placeholder
	newPrompt := strings.ReplaceAll(promptTemplate, "{{assistant_name}}", roleName)

	// Change system prompt if connection supports it
	if conn != nil {
		if err := conn.ChangeSystemPrompt(newPrompt); err != nil {
			if logger != nil {
				logger.Error("切换系统提示词失败", "error", err)
			}
			return types.NewActionResponse(
				types.ActionError,
				"切换角色失败",
				"系统提示词更新失败",
			), err
		}
	}

	if logger != nil {
		logger.Info("准备切换角色", "role", role, "role_name", roleName)
	}

	response := fmt.Sprintf("切换角色成功,我是%s%s", role, roleName)
	return types.NewActionResponse(
		types.ActionDirectResponse,
		"切换角色已处理",
		response,
	), nil
}

// GetInfo returns the eino ToolInfo
func (f *ChangeRoleFunction) GetInfo() *schema.ToolInfo {
	converter := eino_integration.GetGlobalConverter()
	toolInfo, err := converter.ConvertPythonDescToEinoToolInfo(ChangeRoleFunctionName, ChangeRoleFunctionDesc)
	if err != nil {
		log.Errorf("Failed to convert tool info for %s: %v", ChangeRoleFunctionName, err)
		return nil
	}
	return toolInfo
}

// GetType returns the tool type
func (f *ChangeRoleFunction) GetType() types.ToolType {
	return types.ToolTypeChangeSysPrompt
}

// GetName returns the function name
func (f *ChangeRoleFunction) GetName() string {
	return ChangeRoleFunctionName
}

// GetDescription returns the function description
func (f *ChangeRoleFunction) GetDescription() interface{} {
	return ChangeRoleFunctionDesc
}

// GetSupportedRoles returns the list of supported roles
func (f *ChangeRoleFunction) GetSupportedRoles() []string {
	var roles []string
	for role := range rolePrompts {
		roles = append(roles, role)
	}
	return roles
}

// RegisterChangeRoleFunction registers the change role function
func RegisterChangeRoleFunction() error {
	function := NewChangeRoleFunction()
	return tools.RegisterGlobalFunction(ChangeRoleFunctionName, function)
}

// init automatically registers the function when the package is imported
func init() {
	if err := RegisterChangeRoleFunction(); err != nil {
		log.Errorf("Failed to register %s function: %v", ChangeRoleFunctionName, err)
	}
}
