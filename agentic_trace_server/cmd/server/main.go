package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.Default()

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	log.Println("Starting agentic trace server on :8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("Server exited with error: %v", err)
	}
}
