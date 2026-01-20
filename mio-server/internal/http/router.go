package http

import (
	"io/fs"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	appconfig "mio-server/internal/config"
	"mio-server/internal/ws"
	"mio-server/webassets"
)

func NewRouter(cfg appconfig.Config, wsHandler *ws.Handler, logger *zap.Logger) *gin.Engine {
	router := gin.New()
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false
	router.Use(gin.Recovery())
	router.Use(requestLogger(logger))

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	router.GET("/client-ws", func(c *gin.Context) {
		wsHandler.Handle(c.Writer, c.Request)
	})

	if !mountEmbeddedFrontend(router, logger) {
		router.Static("/frontend", cfg.FrontendDir)
		router.Static("/assets", filepath.Join(cfg.FrontendDir, "assets"))
		router.Static("/libs", filepath.Join(cfg.FrontendDir, "libs"))
		router.GET("/", func(c *gin.Context) {
			c.File(filepath.Join(cfg.FrontendDir, "index.html"))
		})
		router.StaticFile("/favicon.ico", filepath.Join(cfg.FrontendDir, "favicon.ico"))
	}

	router.Static("/live2d-models", cfg.Live2DModelsDir)
	router.Static("/backgrounds", cfg.BackgroundsDir)
	router.Static("/bg", cfg.BackgroundsDir)
	router.Static("/avatars", cfg.AvatarsDir)
	router.Static("/web-tool", cfg.WebToolDir)

	return router
}

func mountEmbeddedFrontend(router *gin.Engine, logger *zap.Logger) bool {
	embeddedRoot, err := webassets.Subdir("vtuber")
	if err != nil {
		if logger != nil {
			logger.Warn("failed to load embedded frontend assets; falling back to disk", zap.Error(err))
		}
		return false
	}

	if logger != nil {
		logger.Info("serving embedded frontend assets", zap.String("source", "webassets/vtuber"))
	}

	rootFS := http.FS(embeddedRoot)
	indexHTML, err := fs.ReadFile(embeddedRoot, "index.html")
	if err != nil {
		if logger != nil {
			logger.Warn("missing embedded index.html; falling back to disk", zap.Error(err))
		}
		return false
	}
	router.StaticFS("/frontend", rootFS)
	router.GET("/", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
	})
	router.StaticFileFS("/favicon.ico", "favicon.ico", rootFS)

	mountSubdir := func(route, dir string) {
		sub, subErr := fs.Sub(embeddedRoot, dir)
		if subErr != nil {
			if logger != nil {
				logger.Warn("missing embedded frontend subdir", zap.String("subdir", dir), zap.Error(subErr))
			}
			return
		}
		router.StaticFS(route, http.FS(sub))
	}

	mountSubdir("/assets", "assets")
	mountSubdir("/libs", "libs")

	return true
}

func requestLogger(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		latency := time.Since(start)
		if logger == nil {
			return
		}
		logger.Info("http request",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.String("query", c.Request.URL.RawQuery),
			zap.String("client_ip", c.ClientIP()),
			zap.Int("status", c.Writer.Status()),
			zap.Int("bytes", c.Writer.Size()),
			zap.Duration("latency", latency),
			zap.String("user_agent", c.Request.UserAgent()),
		)
	}
}
