#!/bin/bash

# 启动 xiaozhi manager-api Go 服务
echo "Starting xiaozhi manager-api Go server..."

# 设置工作目录
cd "$(dirname "$0")/.."

# 检查配置文件是否存在
if [ ! -f "configs/config.yaml" ]; then
    echo "Error: Configuration file not found at configs/config.yaml"
    exit 1
fi

# 编译并运行
echo "Building server..."
go build -o bin/server ./cmd/server

if [ $? -ne 0 ]; then
    echo "Error: Failed to build server"
    exit 1
fi

echo "Server built successfully"
echo "Starting server on port 8003..."
echo "================================================================"
echo "🌐 Web界面可访问: http://localhost:8003"
echo "🚀 API接口地址:   http://localhost:8003/xiaozhi"
echo "================================================================"
echo ""
echo "🔐 用户认证接口:"
echo "  GET  /xiaozhi/user/captcha                     - 生成验证码"
echo "  POST /xiaozhi/user/smsVerification             - 发送短信验证码"
echo "  POST /xiaozhi/user/login                       - 用户登录"
echo "  POST /xiaozhi/user/register                    - 用户注册"
echo "  GET  /xiaozhi/user/info                        - 获取用户信息"
echo "  PUT  /xiaozhi/user/change-password             - 修改密码"
echo "  PUT  /xiaozhi/user/retrieve-password           - 找回密码"
echo "  GET  /xiaozhi/user/pub-config                  - 公共配置"
echo ""
echo "📱 设备管理接口:"
echo "  POST /xiaozhi/device/register                  - 设备注册"
echo "  POST /xiaozhi/device/bind/{agentId}/{code}     - 绑定设备"
echo "  GET  /xiaozhi/device/bind/{agentId}            - 获取用户设备"
echo "  POST /xiaozhi/device/unbind                    - 解绑设备"
echo "  PUT  /xiaozhi/device/update/{id}               - 更新设备"
echo "  POST /xiaozhi/device/manual-add                - 手动添加设备"
echo ""
echo "🤖 智能体管理接口:"
echo "  GET  /xiaozhi/agent/list                       - 智能体列表"
echo "  GET  /xiaozhi/agent/{id}                       - 获取智能体详情"
echo "  POST /xiaozhi/agent                            - 创建智能体"
echo "  PUT  /xiaozhi/agent/{id}                       - 更新智能体"
echo "  POST /xiaozhi/agent/delete                     - 删除智能体"
echo "  GET  /xiaozhi/agent/{id}/sessions              - 获取会话列表"
echo "  GET  /xiaozhi/agent/{id}/chat-history/{sessionId} - 获取聊天记录"
echo ""
echo "⚙️ 模型配置接口:"
echo "  GET  /xiaozhi/models/names                     - 模型名称列表"
echo "  GET  /xiaozhi/models/provider                  - 供应器管理"
echo "  GET  /xiaozhi/models/config/{id}               - 获取模型配置"
echo "  GET  /xiaozhi/models/type/{type}/provideTypes  - 获取供应器类型"
echo ""
echo "🎵 音色管理接口:"
echo "  GET  /xiaozhi/ttsVoice                         - 音色分页列表"
echo "  POST /xiaozhi/ttsVoice                         - 创建音色"
echo "  PUT  /xiaozhi/ttsVoice/{id}                    - 更新音色"
echo "  POST /xiaozhi/ttsVoice/delete                  - 删除音色"
echo ""
echo "🔧 配置管理接口:"
echo "  POST /xiaozhi/config/server-base               - 获取服务器配置"
echo "  POST /xiaozhi/config/agent-models              - 获取智能体模型配置"
echo ""
echo "👨‍💼 管理员接口:"
echo "  GET  /xiaozhi/admin/users                      - 用户管理"
echo "  GET  /xiaozhi/admin/device/all                 - 设备管理"
echo "  GET  /xiaozhi/admin/params/page                - 参数管理"
echo "  GET  /xiaozhi/admin/dict/data/page             - 字典管理"
echo "  GET  /xiaozhi/admin/server/server-list         - 服务器列表"
echo "  POST /xiaozhi/admin/server/emit-action         - 服务器操作"
echo ""
echo "🏥 系统接口:"
echo "  GET  /xiaozhi/health                           - 健康检查"
echo ""

./bin/server