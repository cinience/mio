# 通用资源清理管理器

## 概述

本模块提供了一个通用的、松耦合的资源清理管理机制，允许各个组件在程序退出时自动执行资源清理，而无需在上层代码中显式调用特定的清理函数。

## 设计原则

- **松耦合**: 上层代码不需要知道具体的清理实现
- **自动化**: 组件通过init函数自动注册清理函数
- **优先级控制**: 支持清理优先级，确保正确的清理顺序
- **错误处理**: 提供完善的错误处理和日志记录
- **线程安全**: 支持并发环境下的安全使用

## 使用方法

### 1. 注册清理函数

在组件的init函数中注册清理函数：

```go
package mymodule

import (
    "backend-server/internal/util/cleanup"
)

func init() {
    // 注册清理函数
    cleanup.Register("MyModule", cleanup.PriorityMedium, cleanupResources)
}

func cleanupResources() error {
    // 执行资源清理逻辑
    log.Info("清理MyModule资源...")
    // ... 清理代码 ...
    return nil
}
```

### 2. 执行全局清理

在程序退出时调用：

```go
import "backend-server/internal/util/cleanup"

func shutdown() {
    if err := cleanup.Cleanup(); err != nil {
        log.Errorf("资源清理失败: %v", err)
    }
}
```

### 3. 优先级设置

使用预定义的优先级常量：

```go
// 高优先级：业务服务（先清理）
cleanup.Register("WebSocketServer", cleanup.PriorityHigh, cleanupWebSocket)

// 中等优先级：中间件
cleanup.Register("TTSEngine", cleanup.PriorityMedium, cleanupTTS)

// 低优先级：基础设施（后清理）
cleanup.Register("Database", cleanup.PriorityLow, cleanupDB)

// 最低优先级：原生库（最后清理）
cleanup.Register("NativeLib", cleanup.PriorityLowest, cleanupNative)
```

## 优先级说明

清理按照优先级从高到低执行：

- `PriorityHighest` (100): 最高优先级
- `PriorityHigh` (90): 高优先级 - 业务逻辑和服务
- `PriorityMediumHigh` (80): 中高优先级
- `PriorityMedium` (70): 中等优先级 - 中间件和框架组件
- `PriorityMediumLow` (60): 中低优先级
- `PriorityLow` (40): 低优先级 - 基础设施
- `PriorityLowest` (10): 最低优先级 - 原生库和底层资源

## 错误处理

- 单个清理函数失败不会阻止其他清理函数执行
- 支持panic恢复，确保程序不会因清理函数崩溃
- 提供详细的错误日志和执行状态

## 示例：添加新的清理组件

```go
// internal/domain/database/mysql.go
package database

import (
    "backend-server/internal/util/cleanup"
    log "backend-server/internal/logger"
)

var dbConnection *sql.DB

func init() {
    // 注册数据库清理函数
    cleanup.Register("MySQL Database", cleanup.PriorityLow, cleanupDatabase)
}

func cleanupDatabase() error {
    if dbConnection != nil {
        log.Info("关闭MySQL数据库连接...")
        err := dbConnection.Close()
        dbConnection = nil
        if err != nil {
            return fmt.Errorf("关闭数据库连接失败: %w", err)
        }
        log.Info("MySQL数据库连接已关闭")
    }
    return nil
}
```

## 管理接口

提供了便于调试和监控的管理接口：

```go
// 获取已注册的清理函数数量
count := cleanup.Count()

// 列出所有已注册的清理函数
registered := cleanup.ListRegistered()
for _, item := range registered {
    log.Info("已注册清理函数: %s", item)
}

// 清空所有注册（主要用于测试）
cleanup.Clear()
```

## 最佳实践

1. **在init函数中注册**: 确保组件被导入时自动注册清理函数
2. **使用合适的优先级**: 根据组件间的依赖关系设置优先级
3. **错误处理**: 清理函数应该处理自己的错误，避免panic
4. **幂等性**: 清理函数应该支持多次调用而不出错
5. **日志记录**: 提供详细的清理状态日志

## 与现有代码集成

对于现有组件，只需要：

1. 在组件包中添加清理函数注册
2. 在main.go中通过空导入确保init函数执行
3. 移除上层代码中的直接清理调用

这样就能无缝升级到新的清理机制，实现松耦合的架构设计。
