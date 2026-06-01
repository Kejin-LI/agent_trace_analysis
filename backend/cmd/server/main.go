package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

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

	r.Static("/static", "./frontend/static")
	r.StaticFile("/", "./frontend/index.html")
	r.StaticFile("/sessions", "./frontend/sessions.html")
	r.StaticFile("/session-detail", "./frontend/session-detail.html")
	r.StaticFile("/clusters", "./frontend/clusters.html")
	r.StaticFile("/docs", "./frontend/docs.html")
	r.NoRoute(func(c *gin.Context) {
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