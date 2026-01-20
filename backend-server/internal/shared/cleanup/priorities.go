package cleanup

// 清理优先级常量
// 数字越大优先级越高，越先执行
// 建议按照依赖关系设置优先级：被依赖的组件应该最后清理

const (
	// 高优先级 (90-100): 业务逻辑和服务
	PriorityHighest = 100 // 最高优先级
	PriorityHigh    = 90  // 高优先级

	// 中等优先级 (50-89): 中间件和框架组件
	PriorityMediumHigh = 80 // 中高优先级
	PriorityMedium     = 70 // 中等优先级
	PriorityMediumLow  = 60 // 中低优先级

	// 低优先级 (10-49): 基础设施和底层资源
	PriorityLow    = 40 // 低优先级
	PriorityLowest = 10 // 最低优先级

	// 具体组件的建议优先级
	PriorityWebsocketServer = PriorityHigh       // WebSocket服务器
	PriorityManagerAPI      = PriorityHigh       // Manager API服务
	PriorityMQTTAdapter     = PriorityMediumHigh // MQTT适配器
	PriorityTTSEngine       = PriorityMedium     // TTS引擎
	PriorityASREngine       = PriorityMedium     // ASR引擎
	PriorityLLMEngine       = PriorityMedium     // LLM引擎
	PriorityDatabase        = PriorityLow        // 数据库连接
	PriorityNativeLibrary   = PriorityLowest     // 原生库（如C++库）
)

// GetPriorityName 获取优先级的描述名称
func GetPriorityName(priority int) string {
	switch {
	case priority >= 100:
		return "最高优先级"
	case priority >= 90:
		return "高优先级"
	case priority >= 80:
		return "中高优先级"
	case priority >= 70:
		return "中等优先级"
	case priority >= 60:
		return "中低优先级"
	case priority >= 40:
		return "低优先级"
	default:
		return "最低优先级"
	}
}
