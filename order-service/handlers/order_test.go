package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"order-svc/grpc"
	"order-svc/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/IBM/sarama"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// Mock ProductClient for testing.
type mockProductClient struct {
	checkAvailabilityFunc func(ctx context.Context, productID, quantity int32) (bool, int32, error)
	getProductFunc        func(ctx context.Context, productID int32) (any, error)
}

func (m *mockProductClient) CheckAvailability(ctx context.Context, productID, quantity int32) (bool, int32, error) {
	if m.checkAvailabilityFunc != nil {
		return m.checkAvailabilityFunc(ctx, productID, quantity)
	}
	return true, 100, nil
}

func (m *mockProductClient) GetProduct(ctx context.Context, productID int32) (any, error) {
	if m.getProductFunc != nil {
		return m.getProductFunc(ctx, productID)
	}
	// Return a simple struct that matches what the handler expects (has Price field)
	type productResp struct {
		Price float64
	}
	return &productResp{
		Price: 10.99,
	}, nil
}

// Mock Kafka Producer for testing.
type mockProducer struct{}

func (m *mockProducer) SendMessage(msg *sarama.ProducerMessage) (partition int32, offset int64, err error) {
	return 0, 0, nil
}

func (m *mockProducer) SendMessages(msgs []*sarama.ProducerMessage) error {
	return nil
}

func (m *mockProducer) Close() error {
	return nil
}

// Note: setupOrderTest is simplified - CreateOrder tests require refactoring
// the handler to use interfaces for ProductClient and Kafka Producer.
func setupOrderTest(t *testing.T) (*OrderHandler, sqlmock.Sqlmock, *gin.Engine) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}

	logger := zaptest.NewLogger(t, zaptest.Level(zap.InfoLevel))
	// For GetOrder tests, we can use nil for producer and productClient
	// since GetOrder doesn't use them
	var producer sarama.SyncProducer = nil
	var productClient *grpc.ProductClient = nil
	handler := &OrderHandler{
		db:            db,
		producer:      producer,
		productClient: productClient,
		logger:        logger,
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/orders/:id", handler.GetOrder)

	return handler, mock, router
}

// Note: CreateOrder tests are skipped because the handler uses concrete types
// (ProductClient) that are difficult to mock without refactoring to use interfaces.
// The GetOrder tests below work because they only require database mocking.

func TestOrderHandler_GetOrder_Success(t *testing.T) {
	handler, mock, router := setupOrderTest(t)
	defer handler.db.Close()

	// Mock: Get order by ID
	rows := sqlmock.NewRows([]string{"id", "user_id", "product_id", "quantity", "status", "total_price", "created_at", "updated_at"}).
		AddRow(1, 1, 1, 2, models.OrderStatusPending, 21.98, time.Now(), time.Now())

	mock.ExpectQuery("SELECT id, user_id, product_id, quantity, status, total_price, created_at, updated_at FROM orders WHERE id = \\$1").
		WithArgs(1).
		WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/orders/1", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Database expectations were not met: %v", err)
	}
}

func TestOrderHandler_GetOrder_NotFound(t *testing.T) {
	handler, mock, router := setupOrderTest(t)
	defer handler.db.Close()

	// Mock: Order not found
	mock.ExpectQuery("SELECT id, user_id, product_id, quantity, status, total_price, created_at, updated_at FROM orders WHERE id = \\$1").
		WithArgs(999).
		WillReturnError(sql.ErrNoRows)

	req := httptest.NewRequest(http.MethodGet, "/orders/999", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Database expectations were not met: %v", err)
	}
}
