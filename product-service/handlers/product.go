package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"product-svc/cache"
	"product-svc/circuitbreaker"
	"product-svc/models"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

type ProductHandler struct {
	db             *sql.DB
	redisClient    *redis.Client
	logger         *zap.Logger
	circuitBreaker *circuitbreaker.CircuitBreaker
}

func NewProductHandler(db *sql.DB, redisClient *redis.Client, logger *zap.Logger) *ProductHandler {
	return &ProductHandler{
		db:             db,
		redisClient:    redisClient,
		logger:         logger,
		circuitBreaker: circuitbreaker.NewCircuitBreaker(5, 30*time.Second),
	}
}

func (h *ProductHandler) GetProducts(c *gin.Context) {
	ctx, span := otel.Tracer("product-service").Start(c.Request.Context(), "GetProducts")
	defer span.End()

	rows, err := h.db.QueryContext(ctx, "SELECT id, name, price, stock, created_at, updated_at FROM products ORDER BY id")
	if err != nil {
		span.RecordError(err)
		h.logger.Error("Failed to fetch products", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}
	defer rows.Close()

	var products []models.Product
	for rows.Next() {
		var p models.Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Price, &p.Stock, &p.CreatedAt, &p.UpdatedAt); err != nil {
			span.RecordError(err)
			h.logger.Error("Failed to scan product", zap.Error(err))
			continue
		}
		products = append(products, p)
	}

	span.SetAttributes(attribute.Int("products.count", len(products)))
	c.JSON(http.StatusOK, products)
}

func (h *ProductHandler) GetProduct(c *gin.Context) {
	ctx, span := otel.Tracer("product-service").Start(c.Request.Context(), "GetProduct")
	defer span.End()

	id := c.Param("id")
	span.SetAttributes(attribute.String("product.id", id))

	// Try to get from cache first
	cachedData, err := cache.GetProduct(ctx, h.redisClient, id)
	if err == nil {
		var product models.Product
		if err := json.Unmarshal(cachedData, &product); err == nil {
			span.SetAttributes(attribute.Bool("cache.hit", true))
			h.logger.Info("Cache hit", zap.String("product_id", id))
			c.JSON(http.StatusOK, product)
			return
		}
	}
	span.SetAttributes(attribute.Bool("cache.hit", false))

	// Get from database with circuit breaker
	var product models.Product
	dbErr := h.circuitBreaker.Execute(ctx, func() error {
		return h.db.QueryRowContext(ctx,
			"SELECT id, name, price, stock, created_at, updated_at FROM products WHERE id = $1",
			id,
		).Scan(&product.ID, &product.Name, &product.Price, &product.Stock, &product.CreatedAt, &product.UpdatedAt)
	})

	if dbErr != nil {
		if dbErr == circuitbreaker.ErrCircuitOpen {
			span.SetAttributes(attribute.String("circuit.state", "open"))
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Service temporarily unavailable"})
			return
		}
		if dbErr == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
			return
		}
		span.RecordError(dbErr)
		h.logger.Error("Failed to fetch product", zap.Error(dbErr))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	// Cache the product for 5 minutes
	cache.SetProduct(ctx, h.redisClient, id, product, 5*time.Minute)

	c.JSON(http.StatusOK, product)
}

func (h *ProductHandler) CreateProduct(c *gin.Context) {
	ctx, span := otel.Tracer("product-service").Start(c.Request.Context(), "CreateProduct")
	defer span.End()

	var req models.CreateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var product models.Product
	err := h.db.QueryRowContext(ctx,
		"INSERT INTO products (name, price, stock) VALUES ($1, $2, $3) RETURNING id, name, price, stock, created_at, updated_at",
		req.Name, req.Price, req.Stock,
	).Scan(&product.ID, &product.Name, &product.Price, &product.Stock, &product.CreatedAt, &product.UpdatedAt)

	if err != nil {
		span.RecordError(err)
		h.logger.Error("Failed to create product", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	span.SetAttributes(attribute.Int("product.id", product.ID))
	h.logger.Info("Product created", zap.Int("product_id", product.ID))
	c.JSON(http.StatusCreated, product)
}

func (h *ProductHandler) UpdateProduct(c *gin.Context) {
	ctx, span := otel.Tracer("product-service").Start(c.Request.Context(), "UpdateProduct")
	defer span.End()

	id := c.Param("id")
	span.SetAttributes(attribute.String("product.id", id))

	var req models.UpdateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Build update query dynamically
	query := "UPDATE products SET updated_at = CURRENT_TIMESTAMP"
	args := []interface{}{}
	argPos := 1

	if req.Name != "" {
		query += ", name = $" + strconv.Itoa(argPos)
		args = append(args, req.Name)
		argPos++
	}
	if req.Price > 0 {
		query += ", price = $" + strconv.Itoa(argPos)
		args = append(args, req.Price)
		argPos++
	}
	if req.Stock >= 0 {
		query += ", stock = $" + strconv.Itoa(argPos)
		args = append(args, req.Stock)
		argPos++
	}

	query += " WHERE id = $" + strconv.Itoa(argPos) + " RETURNING id, name, price, stock, created_at, updated_at"
	args = append(args, id)

	var product models.Product
	err := h.db.QueryRowContext(ctx, query, args...).Scan(
		&product.ID, &product.Name, &product.Price, &product.Stock, &product.CreatedAt, &product.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
			return
		}
		span.RecordError(err)
		h.logger.Error("Failed to update product", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	// Invalidate cache
	cache.DeleteProduct(ctx, h.redisClient, id)

	h.logger.Info("Product updated", zap.String("product_id", id))
	c.JSON(http.StatusOK, product)
}

func (h *ProductHandler) DeleteProduct(c *gin.Context) {
	ctx, span := otel.Tracer("product-service").Start(c.Request.Context(), "DeleteProduct")
	defer span.End()

	id := c.Param("id")
	span.SetAttributes(attribute.String("product.id", id))

	result, err := h.db.ExecContext(ctx, "DELETE FROM products WHERE id = $1", id)
	if err != nil {
		span.RecordError(err)
		h.logger.Error("Failed to delete product", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		return
	}

	// Invalidate cache
	cache.DeleteProduct(ctx, h.redisClient, id)

	h.logger.Info("Product deleted", zap.String("product_id", id))
	c.JSON(http.StatusOK, gin.H{"message": "Product deleted successfully"})
}
