package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"notification-svc/handlers"
	"notification-svc/kafka"
	"notification-svc/middleware"

	"github.com/IBM/sarama"
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
	// defer consumer.Close()

	// Start Kafka consumer in background
	go func() {
		if err := kafka.StartConsumer(consumer, logger); err != nil {
			logger.Error("Kafka consumer error", zap.Error(err))
		}
	}()

	// Setup REST API with Gin
	router := gin.New()
	router.Use(gin.Recovery())
	// OpenTelemetry middleware must be first to extract trace context
	router.Use(otelgin.Middleware("notification-service"))
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

	gracefulShutdown(srv, consumer, logger)
}

// gracefulShutdown waits for SIGINT/SIGTERM and shuts down HTTP server and Kafka consumer gracefully
func gracefulShutdown(srv *http.Server, consumer sarama.Consumer, logger *zap.Logger) {
	// Channel to listen for interrupt signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Block until a signal is received
	<-quit
	logger.Info("Received shutdown signal. Shutting down...")

	// Context with timeout for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Shutdown HTTP server gracefully
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("HTTP server forced to shutdown", zap.Error(err))
	} else {
		logger.Info("HTTP server shut down gracefully")
	}

	// Close Kafka consumer gracefully
	if consumer != nil {
		if err := consumer.Close(); err != nil {
			logger.Error("Failed to close Kafka consumer", zap.Error(err))
		} else {
			logger.Info("Kafka consumer closed gracefully")
		}
	}

	logger.Info("Service exited gracefully")
}
