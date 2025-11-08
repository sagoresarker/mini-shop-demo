package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"product-svc/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func setupProductTest(t *testing.T) (*ProductHandler, sqlmock.Sqlmock, *gin.Engine) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}

	// Create a mock Redis client (we'll use a real client but with a test context)
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	logger := zaptest.NewLogger(t, zaptest.Level(zap.InfoLevel))
	handler := NewProductHandler(db, redisClient, logger)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/products", handler.GetProducts)
	router.GET("/products/:id", handler.GetProduct)
	router.POST("/products", handler.CreateProduct)
	router.PUT("/products/:id", handler.UpdateProduct)
	router.DELETE("/products/:id", handler.DeleteProduct)

	return handler, mock, router
}

func TestProductHandler_GetProducts_Success(t *testing.T) {
	handler, mock, router := setupProductTest(t)
	defer handler.db.Close()

	// Mock: Get all products
	rows := sqlmock.NewRows([]string{"id", "name", "price", "stock", "created_at", "updated_at"}).
		AddRow(1, "Product 1", 10.99, 100, time.Now(), time.Now()).
		AddRow(2, "Product 2", 20.99, 50, time.Now(), time.Now())

	mock.ExpectQuery("SELECT id, name, price, stock, created_at, updated_at FROM products ORDER BY id").
		WillReturnRows(rows)

	req := httptest.NewRequest("GET", "/products", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Database expectations were not met: %v", err)
	}
}

func TestProductHandler_GetProduct_Success(t *testing.T) {
	handler, mock, router := setupProductTest(t)
	defer handler.db.Close()

	// Mock: Get product by ID
	rows := sqlmock.NewRows([]string{"id", "name", "price", "stock", "created_at", "updated_at"}).
		AddRow(1, "Product 1", 10.99, 100, time.Now(), time.Now())

	mock.ExpectQuery("SELECT id, name, price, stock, created_at, updated_at FROM products WHERE id = \\$1").
		WithArgs("1").
		WillReturnRows(rows)

	req := httptest.NewRequest("GET", "/products/1", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Database expectations were not met: %v", err)
	}
}

func TestProductHandler_GetProduct_NotFound(t *testing.T) {
	handler, mock, router := setupProductTest(t)
	defer handler.db.Close()

	// Mock: Product not found
	mock.ExpectQuery("SELECT id, name, price, stock, created_at, updated_at FROM products WHERE id = \\$1").
		WithArgs("999").
		WillReturnError(sql.ErrNoRows)

	req := httptest.NewRequest("GET", "/products/999", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Database expectations were not met: %v", err)
	}
}

func TestProductHandler_CreateProduct_Success(t *testing.T) {
	handler, mock, router := setupProductTest(t)
	defer handler.db.Close()

	// Mock: Insert product
	rows := sqlmock.NewRows([]string{"id", "name", "price", "stock", "created_at", "updated_at"}).
		AddRow(1, "New Product", 15.99, 200, time.Now(), time.Now())

	mock.ExpectQuery("INSERT INTO products").
		WithArgs("New Product", 15.99, 200).
		WillReturnRows(rows)

	reqBody := models.CreateProductRequest{
		Name:  "New Product",
		Price: 15.99,
		Stock: 200,
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/products", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d", http.StatusCreated, w.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Database expectations were not met: %v", err)
	}
}

func TestProductHandler_UpdateProduct_Success(t *testing.T) {
	handler, mock, router := setupProductTest(t)
	defer handler.db.Close()

	// Mock: Update product
	rows := sqlmock.NewRows([]string{"id", "name", "price", "stock", "created_at", "updated_at"}).
		AddRow(1, "Updated Product", 25.99, 150, time.Now(), time.Now())

	mock.ExpectQuery("UPDATE products SET").
		WithArgs("Updated Product", 25.99, 150, "1").
		WillReturnRows(rows)

	reqBody := models.UpdateProductRequest{
		Name:  "Updated Product",
		Price: 25.99,
		Stock: 150,
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("PUT", "/products/1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Database expectations were not met: %v", err)
	}
}

func TestProductHandler_DeleteProduct_Success(t *testing.T) {
	handler, mock, router := setupProductTest(t)
	defer handler.db.Close()

	// Mock: Delete product
	mock.ExpectExec("DELETE FROM products WHERE id = \\$1").
		WithArgs("1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := httptest.NewRequest("DELETE", "/products/1", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Database expectations were not met: %v", err)
	}
}

func TestProductHandler_DeleteProduct_NotFound(t *testing.T) {
	handler, mock, router := setupProductTest(t)
	defer handler.db.Close()

	// Mock: Product not found
	mock.ExpectExec("DELETE FROM products WHERE id = \\$1").
		WithArgs("999").
		WillReturnResult(sqlmock.NewResult(0, 0))

	req := httptest.NewRequest("DELETE", "/products/999", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Database expectations were not met: %v", err)
	}
}
