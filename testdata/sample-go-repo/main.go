package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

var (
	db  *sql.DB
	rdb *redis.Client
)

func main() {
	// Initialize database
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://localhost/sampledb?sslmode=disable"
	}

	var err error
	db, err = sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Initialize Redis
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "localhost:6379"
	}

	rdb = redis.NewClient(&redis.Options{
		Addr: redisURL,
	})
	defer rdb.Close()

	// Setup router
	r := gin.Default()

	// Health check endpoint
	r.GET("/health", healthCheck)

	// API endpoints
	r.GET("/api/users", getUsers)
	r.POST("/api/users", createUser)
	r.GET("/api/users/:id", getUser)

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting server on port %s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
		"service": "sample-api",
	})
}

func getUsers(c *gin.Context) {
	// TODO: Implement get users from database
	c.JSON(http.StatusOK, gin.H{
		"users": []string{},
	})
}

func createUser(c *gin.Context) {
	// TODO: Implement create user
	c.JSON(http.StatusCreated, gin.H{
		"message": "User created",
	})
}

func getUser(c *gin.Context) {
	id := c.Param("id")
	// TODO: Implement get user by ID
	c.JSON(http.StatusOK, gin.H{
		"id": id,
	})
}