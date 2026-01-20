# Manager API - Go Version
后台管理系统

## 技术栈

- **语言**: Go 1.21+
- **Web框架**: Gin (github.com/gin-gonic/gin)
- **ORM**: GORM (gorm.io/gorm)
- **数据库**: MySQL 8.0+
- **缓存**: Redis 6.0+
- **配置管理**: Viper

## 项目结构

```
manager-api-go/
├── cmd/server/         # 应用程序入口
├── internal/
│   ├── api/            # HTTP API 处理器和路由
│   ├── config/         # 配置文件加载和数据库连接
│   ├── models/         # 数据模型和GORM结构体
│   ├── repository/     # 数据访问层
│   └── service/        # 业务逻辑层
├── configs/            # 配置文件
├── scripts/            # 部署和运行脚本
├── bin/                # 编译后的可执行文件
├── go.mod
└── README.md
```

## 功能特性

1. **系统管理**
   - 用户登录/登出
   - 用户信息查询
   - Token 认证机制

2. **设备管理** ✨ 
   - 设备注册（生成验证码）
   - 设备绑定（智能体）
   - 用户设备列表查询
   - 设备解绑
   - 设备信息更新
   - 手动添加设备
   - 管理员设备查询

3. **管理员功能**
   - 用户分页查询
   - 用户密码重置
   - 用户删除
   - 批量修改用户状态
   - 设备管理（分页查询）

4. **系统参数管理** ✨
   - 参数分页查询
   - 参数详情获取
   - 参数新增/修改
   - 参数批量删除

5. **字典数据管理** ✨
   - 字典数据分页查询
   - 字典数据详情获取
   - 字典数据新增/修改/删除
   - 根据字典类型获取数据列表

6. **智能体管理** ✨
   - 用户智能体列表查询
   - 智能体详情获取
   - 智能体创建/修改/删除
   - 智能体模板管理
   - 聊天记录管理（会话列表、历史记录）
   - 设备记忆更新

7. **模型配置管理** ✨
   - AI模型供应器管理
   - 模型配置CRUD操作
   - 模型启用/禁用
   - 默认模型设置
   - TTS音色管理

8. **配置管理** ✨
   - 服务端基础配置获取
   - 智能体模型配置动态获取
   - 模块配置构建
   - Redis缓存支持

9. **音色管理** ✨
   - 音色分页查询
   - 音色新增/修改/删除
   - 按TTS模型筛选
   - 音色名称列表获取

10. **服务端管理** ✨
    - WebSocket服务端列表获取
    - Python服务端操作通知
    - 服务端配置更新通知
    - 模拟WebSocket客户端通信

11. **基础设施**
    - 数据库连接池
    - Redis 连接
    - 配置管理
    - 中间件（CORS、日志、异常恢复）
    - 标准分层架构（Repository、Service、Handler）
    - 完整的GORM模型定义
    - 统一的API响应格式

12. **静态Web服务** ✨
    - 前端页面托管服务
    - SPA路由支持
    - 静态资源服务
    - Web管理界面访问

### 完成的额外功能

1. **完整登录系统** ✅
   - 验证码生成和验证  
   - 用户注册功能
   - 密码找回功能
   - 短信验证码支持
   - 公共配置接口

2. **静态Web服务** ✅
   - 前端代码托管（dist目录）
   - SPA单页应用路由支持
   - 静态资源文件服务
   - Web管理界面集成

### 待实现功能（可选扩展）

1. **WebSocket实时通信**
   - 真实WebSocket客户端实现
   - 服务端状态实时监控  
   - 配置更新实时推送

## 配置说明

编辑 `config/config.yaml` 文件，配置数据库和 Redis 连接信息：

```yaml
# 服务器配置
server:
  port: 8007
  mode: release  # debug, release, test
  
# 数据库配置
database:
  driver: mysql
  dsn: root:123456@tcp(127.0.0.1:3306)/xiaozhi_esp32_server?charset=utf8mb4&parseTime=True&loc=Local
  max_idle_conns: 10
  max_open_conns: 100
  
# Redis 配置
redis:
  host: localhost
  port: 6379
  password: ""
  db: 0
  pool_size: 10
```

也可以通过环境变量覆盖配置项，例如：

```bash
export XIAOZHI_DATABASE_DSN="root:123456@tcp(127.0.0.1:3306)/xiaozhi_esp32_server?charset=utf8mb4&parseTime=True&loc=Local"
export XIAOZHI_DATABASE_DRIVER=mysql   # 可设置为 mysql、postgres、postgresql

# PostgreSQL 例子（GORM DSN 格式）:
# export XIAOZHI_DATABASE_DSN="host=127.0.0.1 port=5432 user=postgres password=secret dbname=xiaozhi sslmode=disable TimeZone=Asia/Shanghai"
# export XIAOZHI_DATABASE_DRIVER=postgres
```

如果使用 PostgreSQL，可将 `database.driver` 设置为 `postgres`，并采用类似如下的 DSN：

```yaml
database:
  driver: postgres
  dsn: "host=127.0.0.1 port=5432 user=postgres password=secret dbname=xiaozhi sslmode=disable TimeZone=Asia/Shanghai"
```

### 初始数据

服务启动时会自动检测 `sys_user` 是否已有数据；当数据库为空时，将会导入 `migrations/data/*.csv` 中的示例数据以完成初始化（表结构由 GORM 自动迁移生成）。

如需刷新示例数据，可确保 `XIAOZHI_DATABASE_DSN` 指向目标数据库后执行：

```bash
scripts/export_initial_data.sh
```

脚本会重新导出当前数据到 CSV 文件（支持 MySQL 与 PostgreSQL），随后重启服务即可生效。

## 运行方式

### 1. 使用脚本运行

```bash
./scripts/run.sh
```

### 2. 手动运行

```bash
# 编译
go build -o bin/server ./cmd/server

# 运行
./bin/server
```

### 3. 开发模式运行

```bash
go run ./cmd/server
```

## 服务访问

服务启动后，可通过以下方式访问：

### Web管理界面 ✅
- **访问地址**: http://localhost:8003
- **功能**: 完整的前端管理界面（Vue.js应用）
- **特性**: 
  - ✅ SPA单页应用路由支持
  - ✅ 自动服务静态资源文件（JS、CSS、图片、字体）
  - ✅ PWA支持（Service Worker）
  - ✅ 响应式设计，支持移动端访问
  - ✅ 现代化UI界面
  - ✅ 与后端API完美集成

### API 接口
- **基础地址**: http://localhost:8003/xiaozhi
- **格式**: JSON
- **认证**: Token-based authentication

### 系统接口

- `GET /xiaozhi/health` - 健康检查
- `POST /xiaozhi/sys/login` - 用户登录
- `POST /xiaozhi/sys/logout` - 用户登出  
- `GET /xiaozhi/sys/user/info` - 获取用户信息

### 设备管理接口

- `POST /xiaozhi/device/register` - 设备注册
- `POST /xiaozhi/device/bind/{agentId}/{deviceCode}` - 绑定设备
- `GET /xiaozhi/device/bind/{agentId}` - 获取用户设备列表
- `POST /xiaozhi/device/unbind` - 解绑设备
- `PUT /xiaozhi/device/update/{id}` - 更新设备信息
- `POST /xiaozhi/device/manual-add` - 手动添加设备

### 管理员接口

#### 用户管理
- `GET /xiaozhi/admin/users` - 分页查询用户
- `PUT /xiaozhi/admin/users/{id}` - 重置用户密码
- `DELETE /xiaozhi/admin/users/{id}` - 删除用户
- `PUT /xiaozhi/admin/users/changeStatus/{status}` - 批量修改用户状态

#### 设备管理
- `GET /xiaozhi/admin/device/all` - 分页查询设备

#### 系统参数管理
- `GET /xiaozhi/admin/params/page` - 分页查询参数
- `GET /xiaozhi/admin/params/{id}` - 获取参数详情
- `POST /xiaozhi/admin/params` - 新增参数
- `PUT /xiaozhi/admin/params` - 修改参数
- `POST /xiaozhi/admin/params/delete` - 批量删除参数

#### 字典数据管理
- `GET /xiaozhi/admin/dict/data/page` - 分页查询字典数据
- `GET /xiaozhi/admin/dict/data/{id}` - 获取字典数据详情
- `POST /xiaozhi/admin/dict/data/save` - 新增字典数据
- `PUT /xiaozhi/admin/dict/data/update` - 修改字典数据
- `POST /xiaozhi/admin/dict/data/delete` - 批量删除字典数据
- `GET /xiaozhi/admin/dict/data/type/{dictType}` - 根据字典类型获取数据列表

## 兼容性说明

本 Go 版本与原 Java 版本完全兼容：

1. **API 兼容**: 所有 HTTP API 的端点、请求方法、参数和响应格式保持一致
2. **数据库兼容**: 使用相同的数据库表结构，不需要数据迁移
3. **Redis 兼容**: 使用相同的 Key 命名和数据结构

## 开发说明

### 添加新功能

1. 在 `internal/models/` 中定义数据模型
2. 在 `internal/repository/` 中实现数据访问层
3. 在 `internal/service/` 中实现业务逻辑
4. 在 `internal/api/handlers.go` 中添加 HTTP 处理器
5. 在 `internal/api/server.go` 中注册路由

### 数据库迁移

项目使用 GORM 的自动迁移功能。如需添加新表或修改表结构：

1. 在 `internal/models/` 中定义或修改模型
2. 在启动代码中添加 `db.AutoMigrate(&NewModel{})`

服务进程启动时会自动执行以下步骤：

- 运行 GORM 自动迁移，确保表结构处于最新状态；
- 检查关键表是否为空，若为空则自动导入嵌入在二进制中的 `migrations/data/*.csv` 初始数据。

因此无需额外执行独立的迁移命令。

## 注意事项

1. 确保数据库服务（MySQL 或 PostgreSQL）及 Redis 正常运行
2. 数据库需要预先创建（名称在配置文件中指定）
3. 首次运行时会自动创建必要的数据表
4. 生产环境建议修改默认的 JWT 密钥
