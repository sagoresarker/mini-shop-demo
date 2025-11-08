package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"user-svc/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"golang.org/x/crypto/bcrypt"
)

func setupAuthTest(t *testing.T) (*AuthHandler, sqlmock.Sqlmock, *gin.Engine) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}

	logger := zaptest.NewLogger(t, zaptest.Level(zap.InfoLevel))
	handler := NewAuthHandler(db, logger)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/register", handler.Register)
	router.POST("/login", handler.Login)

	return handler, mock, router
}

func TestAuthHandler_Register_Success(t *testing.T) {
	handler, mock, router := setupAuthTest(t)
	defer handler.db.Close()

	// Mock: Check if user exists (should return no rows)
	mock.ExpectQuery("SELECT id FROM users WHERE email = \\$1").
		WithArgs("test@example.com").
		WillReturnError(sql.ErrNoRows)

	// Mock: Insert user
	mock.ExpectQuery("INSERT INTO users").
		WithArgs("testuser", "test@example.com", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "email", "created_at"}).
			AddRow(1, "testuser", "test@example.com", time.Now()))

	reqBody := models.RegisterRequest{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/register", bytes.NewBuffer(body))
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

func TestAuthHandler_Register_UserExists(t *testing.T) {
	handler, mock, router := setupAuthTest(t)
	defer handler.db.Close()

	// Mock: User already exists
	mock.ExpectQuery("SELECT id FROM users WHERE email = \\$1").
		WithArgs("test@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	reqBody := models.RegisterRequest{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("Expected status %d, got %d", http.StatusConflict, w.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Database expectations were not met: %v", err)
	}
}

func TestAuthHandler_Register_MissingName(t *testing.T) {
	handler, mock, router := setupAuthTest(t)
	defer handler.db.Close()

	// No database expectations - should return early before any DB calls
	reqBody := models.RegisterRequest{
		Email:    "test@example.com",
		Password: "password123",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	// Check that no database calls were made
	// ExpectationsWereMet will return an error if there were unexpected calls
	// But since we set no expectations, it should return nil if no calls were made
	// However, if calls were made, it will return an error about unmet expectations
	// So we need to check that no queries were executed
	if err := mock.ExpectationsWereMet(); err != nil {
		// If there's an error, it means there were unexpected calls
		t.Errorf("Unexpected database calls were made: %v", err)
	}
}

func TestAuthHandler_Login_Success(t *testing.T) {
	handler, mock, router := setupAuthTest(t)
	defer handler.db.Close()

	// Mock: Get user from database
	hashedPassword, _ := hashPassword("password123")
	mock.ExpectQuery("SELECT id, name, email, password_hash, created_at FROM users WHERE email = \\$1").
		WithArgs("test@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "email", "password_hash", "created_at"}).
			AddRow(1, "testuser", "test@example.com", hashedPassword, time.Now()))

	reqBody := models.LoginRequest{
		Email:    "test@example.com",
		Password: "password123",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/login", bytes.NewBuffer(body))
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

func TestAuthHandler_Login_InvalidCredentials(t *testing.T) {
	handler, mock, router := setupAuthTest(t)
	defer handler.db.Close()

	// Mock: User not found
	mock.ExpectQuery("SELECT id, name, email, password_hash, created_at FROM users WHERE email = \\$1").
		WithArgs("test@example.com").
		WillReturnError(sql.ErrNoRows)

	reqBody := models.LoginRequest{
		Email:    "test@example.com",
		Password: "wrongpassword",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Database expectations were not met: %v", err)
	}
}

// Helper function to hash password for testing
func hashPassword(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hashed), err
}
