package main

import (
	"context"
	"database/sql"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"order-svc/database"
	"order-svc/grpc"
	"order-svc/handlers"
	"order-svc/kafka"
	"order-svc/middleware"
	order "order-svc/proto"

	"github.com/IBM/sarama"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.uber.org/zap"
	grpcLib "google.golang.org/grpc"
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
	consumer, err := kafka.InitConsumer(logger)
	if err != nil {
		logger.Fatal("Failed to initialize Kafka consumer", zap.Error(err))
	}
	defer consumer.Close()

	// Kafka shutdown context
	consumerCtx, consumerCancel := context.WithCancel(context.Background())
	go func() {
		if err := kafka.StartConsumerWithContext(consumerCtx, consumer, db, logger); err != nil {
			logger.Error("Kafka consumer stopped", zap.Error(err))
		}
	}()

	// Initialize OpenTelemetry
	shutdown, err := middleware.InitTracing("order-service")
	if err != nil {
		logger.Fatal("Failed to initialize tracing", zap.Error(err))
	}
	defer shutdown()

	// Initialize gRPC client for Product Service
	productClient, err := grpc.InitProductClient(logger)
	if err != nil {
		logger.Fatal("Failed to initialize Product gRPC client", zap.Error(err))
	}
	defer productClient.Close()

	// Setup REST API with Gin
	router := gin.New()
	router.Use(gin.Recovery())
	// OpenTelemetry middleware must be first to extract trace context
	router.Use(otelgin.Middleware("order-service"))
	router.Use(middleware.LoggerMiddleware(logger))
	router.Use(middleware.MetricsMiddleware())

	// Health check endpoint
	router.GET("/health", handlers.HealthCheck)

	// Metrics endpoint
	router.GET("/metrics", middleware.PrometheusHandler())

	// Order endpoints
	orderHandler := handlers.NewOrderHandler(db, producer, productClient, logger)
	router.POST("/orders", orderHandler.CreateOrder)
	router.GET("/orders/:id", orderHandler.GetOrder)

	// Start REST server
	restSrv := &http.Server{
		Addr:    ":8082",
		Handler: router,
	}

	go func() {
		if err := restSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Failed to start REST server", zap.Error(err))
		}
	}()

	logger.Info("Order Service REST API started on :8082")

	// Start gRPC server
	grpcListener, err := net.Listen("tcp", ":50051")
	if err != nil {
		logger.Fatal("Failed to listen on gRPC port", zap.Error(err))
	}

	grpcServer := grpcLib.NewServer(
		grpcLib.StatsHandler(otelgrpc.NewServerHandler()),
	)
	orderService := handlers.NewOrderService(db, producer, productClient, logger)
	order.RegisterOrderServiceServer(grpcServer, orderService)

	go func() {
		if err := grpcServer.Serve(grpcListener); err != nil {
			logger.Fatal("Failed to start gRPC server", zap.Error(err))
		}
	}()

	logger.Info("Order Service gRPC server started on :50051")

	// Call graceful shutdown function
	gracefulShutdown(restSrv, grpcServer, consumerCancel, consumer, producer, productClient, db, shutdown, logger)

}

func gracefulShutdown(restSrv *http.Server, grpcServer *grpcLib.Server, consumerCancel context.CancelFunc, consumer sarama.Consumer, producer sarama.SyncProducer, productClient *grpc.ProductClient, db *sql.DB, shutdownTracing func(), logger *zap.Logger) {
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

	// Stop Kafka consumer
	consumerCancel() // signals the goroutine to exit
	if err := consumer.Close(); err != nil {
		logger.Error("Failed to close Kafka consumer", zap.Error(err))
	} else {
		logger.Info("Kafka consumer stopped gracefully")
	}

	// Close Kafka producer
	if err := producer.Close(); err != nil {
		logger.Error("Failed to close Kafka producer", zap.Error(err))
	} else {
		logger.Info("Kafka producer stopped gracefully")
	}

	// Close gRPC clients
	if err := productClient.Close(); err != nil {
		logger.Error("Failed to close Product gRPC client", zap.Error(err))
	} else {
		logger.Info("Product gRPC client closed gracefully")
	}

	// Close DB connection
	if err := db.Close(); err != nil {
		logger.Error("Failed to close database", zap.Error(err))
	} else {
		logger.Info("Database connection closed gracefully")
	}

	// Shutdown tracing
	shutdownTracing()
	logger.Info("Order Service exited gracefully")
}
