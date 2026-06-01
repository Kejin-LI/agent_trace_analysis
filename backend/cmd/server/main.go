package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

var pageMap = map[string]string{
	"/":              "index.html",
	"/sessions":      "sessions.html",
	"/session-detail":"session-detail.html",
	"/clusters":      "clusters.html",
	"/docs":          "docs.html",
}

func main() {
	r := gin.Default()

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

	r.NoRoute(func(c *gin.Context) {
		requestPath := c.Request.URL.Path
		var filePath string
		if mapped, ok := pageMap[requestPath]; ok {
			filePath = filepath.Join("./frontend", mapped)
		} else {
			filePath = filepath.Join("./frontend", requestPath)
		}
		if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
			c.File(filePath)
			return
		}
		c.File("./frontend/index.html")
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
