package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"payment-svc/database"
	"payment-svc/handlers"
	"payment-svc/kafka"
	"payment-svc/middleware"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.uber.org/zap"
)

func main() {
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	// Initialize database
	db, err := database.InitDB(logger)
	if err != nil {
		logger.Fatal("Failed to initialize database", zap.Error(err))
	}
	defer db.Close()

	// Initialize Kafka producer
	producer, err := kafka.InitProducer(logger)
	if err != nil {
		logger.Fatal("Failed to initialize Kafka producer", zap.Error(err))
	}
	defer producer.Close()

	// Initialize Kafka consumer
	consumerGroup, err := kafka.InitConsumer(logger)
	if err != nil {
		logger.Fatal("Failed to initialize Kafka consumer", zap.Error(err))
	}
	defer consumerGroup.Close()

	// Initialize OpenTelemetry
	shutdown, err := middleware.InitTracing("payment-service")
	if err != nil {
		logger.Fatal("Failed to initialize tracing", zap.Error(err))
	}
	defer shutdown()

	// Start Kafka consumer in background
	consumerCtx, consumerCancel := context.WithCancel(context.Background())
	defer consumerCancel()

	var consumerWG sync.WaitGroup
	consumerWG.Add(1)
	go func() {
		defer consumerWG.Done()
		if err := kafka.StartConsumer(consumerCtx, consumerGroup, db, producer, logger); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("Kafka consumer error", zap.Error(err))
		}
	}()

	// Setup REST API with Gin
	router := gin.New()
	router.Use(gin.Recovery())
	// OpenTelemetry middleware must be first to extract trace context
	router.Use(otelgin.Middleware("payment-service"))
	router.Use(middleware.LoggerMiddleware(logger))
	router.Use(middleware.MetricsMiddleware())

	// Health check endpoint
	router.GET("/health", handlers.HealthCheck)

	// Metrics endpoint
	router.GET("/metrics", middleware.PrometheusHandler())

	// Start REST server
	srv := &http.Server{
		Addr:    ":8083",
		Handler: router,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Failed to start REST server", zap.Error(err))
		}
	}()

	logger.Info("Payment Service started on :8083")

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	consumerCancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("Failed to shutdown REST server gracefully", zap.Error(err))
	}

	consumerWG.Wait()
	logger.Info("Server exited")
}
