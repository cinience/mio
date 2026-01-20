// @title 小智设备管理API
// @version 1.0
// @description 小智设备管理系统API文档
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url http://www.swagger.io/support
// @contact.email support@swagger.io

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @host localhost:8003
// @BasePath /xiaozhi

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token.

package main

import (
	"flag"

	"manager-server/internal/api"
	"manager-server/internal/config"
	"manager-server/internal/logger"

	_ "manager-server/internal/ginwrapper"
)

func main() {
	// 通过命令行参数指定配置文件路径
	var configPath string
	flag.StringVar(&configPath, "config", "config/config.yaml", "配置文件路径")
	flag.Parse()

	// 加载配置
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		logger.Fatalf("Failed to load config: %v", err)
	}

	if err := logger.Setup(cfg.Log); err != nil {
		logger.Fatalf("Failed to configure logger: %v", err)
	}

	// 初始化数据库
	db, err := config.InitDB(&cfg.Database)
	if err != nil {
		logger.Fatalf("Failed to initialize database: %v", err)
	}

	// 启动服务器
	server := api.NewServer(cfg, db)
	if err := server.Run(); err != nil {
		logger.Fatalf("Failed to start server: %v", err)
	}
}
