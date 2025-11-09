package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"order-svc/grpc"
	"order-svc/kafka"
	"order-svc/models"

	"github.com/IBM/sarama"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

type OrderHandler struct {
	db            *sql.DB
	producer      sarama.SyncProducer
	productClient *grpc.ProductClient
	logger        *zap.Logger
}

func NewOrderHandler(
	db *sql.DB,
	producer sarama.SyncProducer,
	productClient *grpc.ProductClient,
	logger *zap.Logger,
) *OrderHandler {
	return &OrderHandler{
		db:            db,
		producer:      producer,
		productClient: productClient,
		logger:        logger,
	}
}

func (h *OrderHandler) CreateOrder(c *gin.Context) {
	ctx, span := otel.Tracer("order-service").Start(c.Request.Context(), "CreateOrder")
	defer span.End()

	var req models.CreateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	span.SetAttributes(
		attribute.Int("user_id", req.UserID),
		attribute.Int("product_id", req.ProductID),
		attribute.Int("quantity", req.Quantity),
	)

	// Check product availability via gRPC
	available, stock, err := h.productClient.CheckAvailability(ctx, int32(req.ProductID), int32(req.Quantity))
	if err != nil {
		span.RecordError(err)
		h.logger.Error("Failed to check product availability", zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Product service unavailable"})
		return
	}

	if !available {
		span.SetAttributes(attribute.Bool("available", false))
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Product not available",
			"stock": stock,
		})
		return
	}

	// Get product details to calculate total price
	productResp, err := h.productClient.GetProduct(ctx, int32(req.ProductID))
	if err != nil {
		span.RecordError(err)
		h.logger.Error("Failed to get product details", zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Product service unavailable"})
		return
	}

	totalPrice := float64(req.Quantity) * float64(productResp.GetPrice())

	// Create order in database
	var order models.Order
	err = h.db.QueryRowContext(
		ctx,
		"INSERT INTO orders (user_id, product_id, quantity, status, total_price) VALUES ($1, $2, $3, $4, $5) RETURNING id, user_id, product_id, quantity, status, total_price, created_at, updated_at",
		req.UserID,
		req.ProductID,
		req.Quantity,
		models.OrderStatusPending,
		totalPrice,
	).Scan(&order.ID, &order.UserID, &order.ProductID, &order.Quantity, &order.Status, &order.TotalPrice, &order.CreatedAt, &order.UpdatedAt)

	if err != nil {
		span.RecordError(err)
		h.logger.Error("Failed to create order", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	span.SetAttributes(attribute.Int("order.id", order.ID))

	// Publish order_created event to Kafka
	event := models.OrderEvent{
		OrderID:    order.ID,
		UserID:     order.UserID,
		ProductID:  order.ProductID,
		Quantity:   order.Quantity,
		Status:     order.Status,
		TotalPrice: order.TotalPrice,
		EventType:  "order_created",
	}

	if err := kafka.PublishOrderEvent(h.producer, "order_events", event, h.logger); err != nil {
		h.logger.Error("Failed to publish order_created event", zap.Error(err))
		// Don't fail the request, but log the error
	}

	// Simulate payment processing (Saga pattern)
	// In a real scenario, this would call a payment service
	// For now, we'll simulate it and publish appropriate events
	go h.processPayment(ctx, order)

	h.logger.Info("Order created", zap.Int("order_id", order.ID))
	c.JSON(http.StatusCreated, order)
}

func (h *OrderHandler) processPayment(ctx context.Context, order models.Order) {
	// Simulate payment processing
	// In a real scenario, this would call a payment service
	// For demo purposes, we'll randomly fail some payments

	event := models.OrderEvent{
		OrderID:    order.ID,
		UserID:     order.UserID,
		ProductID:  order.ProductID,
		Quantity:   order.Quantity,
		Status:     order.Status,
		TotalPrice: order.TotalPrice,
	}

	// Simulate: 80% success rate
	// In production, this would be an actual payment service call
	if order.ID%5 == 0 {
		// Simulate payment failure
		event.EventType = "payment_failed"
		event.Status = models.OrderStatusFailed
	} else {
		// Simulate payment success
		event.EventType = "order_paid"
		event.Status = models.OrderStatusPaid
	}

	if err := kafka.PublishOrderEvent(h.producer, "order_events", event, h.logger); err != nil {
		h.logger.Error("Failed to publish payment event", zap.Error(err))
	}
}

func (h *OrderHandler) GetOrder(c *gin.Context) {
	ctx, span := otel.Tracer("order-service").Start(c.Request.Context(), "GetOrder")
	defer span.End()

	id := c.Param("id")
	orderID, err := strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid order ID"})
		return
	}

	span.SetAttributes(attribute.Int("order.id", orderID))

	var order models.Order
	err = h.db.QueryRowContext(
		ctx,
		"SELECT id, user_id, product_id, quantity, status, total_price, created_at, updated_at FROM orders WHERE id = $1",
		orderID,
	).Scan(&order.ID, &order.UserID, &order.ProductID, &order.Quantity, &order.Status, &order.TotalPrice, &order.CreatedAt, &order.UpdatedAt)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
			return
		}
		span.RecordError(err)
		h.logger.Error("Failed to get order", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	c.JSON(http.StatusOK, order)
}
