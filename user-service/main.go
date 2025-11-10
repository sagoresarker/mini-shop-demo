package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"user-svc/database"
	"user-svc/handlers"
	"user-svc/middleware"

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

	// Initialize OpenTelemetry
	shutdownTracing, err := middleware.InitTracing("user-service")
	if err != nil {
		logger.Fatal("Failed to initialize tracing", zap.Error(err))
	}

	defer shutdownTracing()

	// Setup Gin router
	router := gin.New()
	router.Use(gin.Recovery())
	// OpenTelemetry middleware must be first to extract trace context
	router.Use(otelgin.Middleware("user-service"))
	router.Use(middleware.LoggerMiddleware(logger))
	router.Use(middleware.MetricsMiddleware())

	// Health check endpoint
	router.GET("/health", handlers.HealthCheck)

	// Metrics endpoint
	router.GET("/metrics", middleware.PrometheusHandler())

	// Auth endpoints
	authHandler := handlers.NewAuthHandler(db, logger)
	router.POST("/register", authHandler.Register)
	router.POST("/login", authHandler.Login)

	// Protected endpoints
	protected := router.Group("/")
	protected.Use(middleware.AuthMiddleware())
	{
		protected.GET("/profile", handlers.GetProfile)
	}

	// Start server
	srv := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	// Graceful shutdown
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Failed to start server", zap.Error(err))
		}
	}()

	logger.Info("User Service started on :8080")

	// Call graceful shutdown
	gracefulShutdown(srv, db, shutdownTracing, logger)
}

// gracefulShutdown handles SIGINT/SIGTERM and shuts down all services gracefully
func gracefulShutdown(srv *http.Server, db *sql.DB, shutdownTracing func(), logger *zap.Logger) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("Shutdown signal received. Exiting...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Shutdown HTTP server
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("HTTP server forced to shutdown", zap.Error(err))
	} else {
		logger.Info("HTTP server stopped gracefully")
	}

	// Close database
	if err := db.Close(); err != nil {
		logger.Error("Failed to close database", zap.Error(err))
	} else {
		logger.Info("Database connection closed gracefully")
	}

	// Shutdown tracing
	shutdownTracing()
	logger.Info("User Service exited gracefully")
}
