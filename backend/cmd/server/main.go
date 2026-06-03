package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/api"
	"code.byted.org/aidp-playground/agentic_trace_server/internal/db"
)

var pageMap = map[string]string{
	"/":              "index.html",
	"/sessions":      "sessions.html",
	"/session-detail":"session-detail.html",
	"/clusters":      "clusters.html",
	"/docs":          "docs.html",
}

func findFrontendDir() string {
	candidates := []string{"./frontend", "../frontend"}
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return "./frontend"
}

var frontendDir string

func main() {
	frontendDir = findFrontendDir()
	r := gin.Default()

	r.Use(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Header("Access-Control-Allow-Origin", "*")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
			c.Header("Access-Control-Allow-Methods", "GET, OPTIONS")
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
		}
		c.Next()
	})

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong from agentic_trace_server",
		})
	})

	// 读库 API：DB 不可用时（缺少环境变量）仅跳过 API，静态服务照常运行。
	if gdb, err := db.Open(); err != nil {
		log.Printf("数据库未连接，读库 API 已禁用: %v", err)
	} else {
		api.New(gdb).Register(r)
		log.Printf("读库 API 已启用，挂载于 /api")
	}

	r.NoRoute(func(c *gin.Context) {
		requestPath := c.Request.URL.Path
		var filePath string
		if mapped, ok := pageMap[requestPath]; ok {
			filePath = filepath.Join(frontendDir, mapped)
		} else {
			filePath = filepath.Join(frontendDir, requestPath)
		}
		if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
			c.File(filePath)
			return
		}
		c.File(filepath.Join(frontendDir, "index.html"))
	})

	port := os.Getenv("TCE_PRIMARY_PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
