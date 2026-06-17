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
	"code.byted.org/aidp-playground/agentic_trace_server/internal/secret"
)

var pageMap = map[string]string{
	"/":               "index.html",
	"/sessions":       "sessions.html",
	"/session-detail": "session-detail.html",
	"/clusters":       "clusters.html",
	"/docs":           "docs.html",
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

	// 本地调试真实 session 预览时，允许跳过 TCC 初始化，避免因内网依赖阻塞整个服务启动。
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("AGENTTRACE_SKIP_TCC")), "1") &&
		!strings.EqualFold(strings.TrimSpace(os.Getenv("AGENTTRACE_SKIP_TCC")), "true") {
		// 初始化 TCC 客户端：AI 一键诊断需从 TCC 读取火山方舟（豆包2.0）加密配置。
		secret.InitTCC()
	} else {
		log.Printf("AGENTTRACE_SKIP_TCC enabled: skip TCC init for local preview")
	}

	r := gin.Default()

	r.Use(func(c *gin.Context) {
		p := c.Request.URL.Path
		// 同时覆盖本地 /api/ 与生产网关 /trace_sever/api/ 两个前缀。
		if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/trace_sever/api/") {
			c.Header("Access-Control-Allow-Origin", "*")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
			c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
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

	// 读库 API：根据 DATA_SOURCE 决定数据源。
	//   DATA_SOURCE=api ：上游接口模式，优先启用 DB-backed 聚合缓存（DB 失败则降级）
	//   其他          ：依赖 DB 的 fornax / tos 模式（DB 不可用则跳过 API）
	if strings.EqualFold(strings.TrimSpace(os.Getenv("DATA_SOURCE")), "api") {
		gdb, err := db.Open()
		if err != nil {
			log.Printf("api 模式数据库未连接，DB-backed 聚合缓存已禁用: %v", err)
		}

		h, err := api.NewAPI(gdb, err)
		if err != nil {
			log.Printf("api 模式初始化失败，读库 API 已禁用: %v", err)
		} else {
			h.Register(r)
			if gdb != nil {
				log.Printf("读库 API 已启用（DATA_SOURCE=api，直连上游模型日志接口，DB 聚合缓存已启用）")
			} else {
				log.Printf("读库 API 已启用（DATA_SOURCE=api，直连上游模型日志接口，DB 聚合缓存未启用）")
			}
		}
	} else if gdb, err := db.Open(); err != nil {
		log.Printf("数据库未连接，读库 API 已禁用: %v", err)
	} else {
		api.New(gdb).Register(r)
		log.Printf("读库 API 已启用，挂载于 /api")
	}

	r.NoRoute(func(c *gin.Context) {
		requestPath := c.Request.URL.Path
		// 生产网关前缀 /trace_sever 不做 path rewrite，请求会原样带前缀打到我们 Pod；
		// 这里把前缀剥掉再走和本地一致的页面映射 / 静态文件查找逻辑。
		const gatewayPrefix = "/trace_sever"
		if strings.HasPrefix(requestPath, gatewayPrefix) {
			stripped := strings.TrimPrefix(requestPath, gatewayPrefix)
			if stripped == "" {
				stripped = "/"
			}
			requestPath = stripped
		}
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
