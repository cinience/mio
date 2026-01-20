package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"backend-server/internal/app"
	"backend-server/internal/config"
	log "backend-server/internal/infrastructure/logger"
	"backend-server/internal/shared/cleanup"
)

// 通过构建时的 -ldflags 设置版本信息
var (
	version = "unknown"
)

func main() {
	// 设置日志模块的版本号
	log.Version = version

	// 添加panic恢复机制，确保即使发生panic也能清理C++资源
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("程序发生panic: %v", r)
			// 打印堆栈跟踪
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			log.Errorf("Stack trace:\n%s", buf[:n])

			// 执行全局资源清理
			log.Info("panic恢复: 执行全局资源清理...")
			if err := cleanup.Cleanup(); err != nil {
				log.Errorf("panic恢复过程中资源清理失败: %v", err)
			} else {
				log.Info("panic恢复: 全局资源清理完成")
			}
		}
	}()

	// 添加正常退出时的资源清理（备用机制）
	defer func() {
		log.Info("程序退出: 执行最终的全局资源清理（备用机制）...")
		if err := cleanup.Cleanup(); err != nil {
			log.Errorf("最终资源清理失败: %v", err)
		} else {
			log.Info("程序退出: 最终全局资源清理完成")
		}
	}()

	// 解析命令行参数
	configFile := flag.String("config", "config/config.yaml", "配置文件路径")
	versionFlag := flag.Bool("version", false, "显示版本信息")
	flag.Parse()

	// 如果指定了-version参数，则只打印版本信息并退出
	if *versionFlag {
		fmt.Printf("AI服务器版本: %s\n", version)
		return
	}

	log.Info("启动AI服务器...")

	if *configFile == "" {
		log.Error("配置文件路径不能为空")
		return
	}

	err := Init(*configFile)
	if err != nil {
		log.Error("初始化失败: ", err)
		return
	}

	// 获取配置
	cfg := config.GetConfig()

	// 根据配置启动pprof服务
	if cfg.Server.PProf.Enable {
		pprofPort := cfg.Server.PProf.Port
		go func() {
			log.Infof("启动pprof服务，端口: %d", pprofPort)
			if err := http.ListenAndServe(fmt.Sprintf(":%d", pprofPort), nil); err != nil {
				log.Errorf("pprof服务启动失败: %v", err)
			}
		}()
		log.Infof("pprof地址: http://localhost:%d/debug/pprof/", pprofPort)
	} else {
		log.Info("pprof服务已禁用")
	}

	// 创建服务器
	appInstance := app.NewApp()
	if appInstance == nil {
		log.Error("服务器创建失败")
		return
	}

	go appInstance.Run()

	// 阻塞监听退出信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	log.Info("服务器已启动，按 Ctrl+C 退出")
	<-quit

	log.Info("正在关闭服务器...")

	// 创建一个带超时的context用于优雅关闭
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 优雅关闭应用
	if err := appInstance.Shutdown(ctx); err != nil {
		log.Warnf("服务器关闭失败: %v", err)
	}

	log.Info("服务器已关闭")
}
