package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"notification-svc/handlers"
	"notification-svc/kafka"
	"notification-svc/middleware"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func main() {
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	// Initialize OpenTelemetry
	shutdown, err := middleware.InitTracing("notification-service")
	if err != nil {
		logger.Fatal("Failed to initialize tracing", zap.Error(err))
	}
	defer shutdown()

	// Initialize Kafka consumer
	consumer, err := kafka.InitConsumer(logger)
	if err != nil {
		logger.Fatal("Failed to initialize Kafka consumer", zap.Error(err))
	}
	defer consumer.Close()

	// Start Kafka consumer in background
	go func() {
		if err := kafka.StartConsumer(consumer, logger); err != nil {
			logger.Error("Kafka consumer error", zap.Error(err))
		}
	}()

	// Setup REST API with Gin
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(middleware.LoggerMiddleware(logger))
	router.Use(middleware.MetricsMiddleware())

	// Health check endpoint
	router.GET("/health", handlers.HealthCheck)

	// Metrics endpoint
	router.GET("/metrics", middleware.PrometheusHandler())

	// Start REST server
	srv := &http.Server{
		Addr:    ":8084",
		Handler: router,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Failed to start REST server", zap.Error(err))
		}
	}()

	logger.Info("Notification Service started on :8084")

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")
	logger.Info("Server exited")
}
