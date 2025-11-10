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

	"product-svc/cache"
	"product-svc/database"
	"product-svc/handlers"
	"product-svc/middleware"
	product "product-svc/proto"

	"net"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.uber.org/zap"
	"google.golang.org/grpc"
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

	// Initialize Redis cache
	redisClient, err := cache.InitRedis(logger)
	if err != nil {
		logger.Fatal("Failed to initialize Redis", zap.Error(err))
	}
	defer redisClient.Close()

	// Initialize OpenTelemetry
	shutdownTracing, err := middleware.InitTracing("product-service")
	if err != nil {
		logger.Fatal("Failed to initialize tracing", zap.Error(err))
	}

	// Setup Gin router
	router := gin.New()
	router.Use(gin.Recovery())
	// OpenTelemetry middleware must be first to extract trace context
	router.Use(otelgin.Middleware("product-service"))
	router.Use(middleware.LoggerMiddleware(logger))
	router.Use(middleware.MetricsMiddleware())

	// Health check endpoint
	router.GET("/health", handlers.HealthCheck)

	// Metrics endpoint
	router.GET("/metrics", middleware.PrometheusHandler())

	// Product endpoints
	productHandler := handlers.NewProductHandler(db, redisClient, logger)
	router.GET("/products", productHandler.GetProducts)
	router.GET("/products/:id", productHandler.GetProduct)
	router.POST("/products", productHandler.CreateProduct)
	router.PUT("/products/:id", productHandler.UpdateProduct)
	router.DELETE("/products/:id", productHandler.DeleteProduct)

	// Start server
	restSrv := &http.Server{
		Addr:    ":8081",
		Handler: router,
	}

	// Graceful shutdown
	go func() {
		if err := restSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Failed to start server", zap.Error(err))
		}
	}()

	logger.Info("Product Service REST API started on :8081")

	// Start gRPC server
	grpcListener, err := net.Listen("tcp", ":50052")
	if err != nil {
		logger.Fatal("Failed to listen on gRPC port", zap.Error(err))
	}

	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
	productService := handlers.NewProductService(db, logger)
	product.RegisterProductServiceServer(grpcServer, productService)

	go func() {
		if err := grpcServer.Serve(grpcListener); err != nil {
			logger.Fatal("Failed to start gRPC server", zap.Error(err))
		}
	}()

	logger.Info("Product Service gRPC server started on :50052")

	// Call graceful shutdown function
	gracefulShutdown(restSrv, grpcServer, db, redisClient, shutdownTracing, logger)
}

// gracefulShutdown handles SIGINT/SIGTERM and shuts down all services gracefully
func gracefulShutdown(
	restSrv *http.Server,
	grpcServer *grpc.Server,
	db *sql.DB,
	redisClient *redis.Client,
	shutdownTracing func(),
	logger *zap.Logger,
) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("Shutdown signal received. Exiting...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Stop REST server
	if err := restSrv.Shutdown(ctx); err != nil {
		logger.Error("REST server forced to shutdown", zap.Error(err))
	} else {
		logger.Info("REST server stopped gracefully")
	}

	// Stop gRPC server
	grpcServer.GracefulStop()
	logger.Info("gRPC server stopped gracefully")

	// Close database
	if err := db.Close(); err != nil {
		logger.Error("Failed to close database", zap.Error(err))
	} else {
		logger.Info("Database connection closed gracefully")
	}

	// Close Redis cache
	if err := redisClient.Close(); err != nil {
		logger.Error("Failed to close Redis cache", zap.Error(err))
	} else {
		logger.Info("Redis cache closed gracefully")
	}

	// Shutdown tracing
	shutdownTracing()
	logger.Info("Product Service exited gracefully")
}
