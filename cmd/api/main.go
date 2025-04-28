package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No env file found")
	}

	rabbitMQURL := os.Getenv("RABBITMQ_URL")
	databaseURL := os.Getenv("DATABASE_URL")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("RabbitMQ URL: %s", rabbitMQURL)
	log.Printf("Database URL: %s", databaseURL)

	router := gin.Default()

	router.POST("/tasks", func(c *gin.Context) {
		log.Println("Received POST /tasks request")
		c.JSON(http.StatusNotImplemented, gin.H{"message": "Task submission not implemented yet"})
	})

	router.GET("/tasks/:id/status", func(c *gin.Context) {
		taskID := c.Param("id")
		log.Printf("Received GET /tasks/%s/status request", taskID)
		c.JSON(http.StatusNotImplemented, gin.H{
			"task_id": taskID,
			"message": "Task status check not implemented yet",
		})
	})

	serverAddr := ":" + port
	log.Printf("Starting API server on %s", serverAddr)
	if err := router.Run(serverAddr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
