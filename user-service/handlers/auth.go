package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"user-svc/middleware"
	"user-svc/models"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	db     *sql.DB
	logger *zap.Logger
}

var jwtSecret = []byte("your-secret-key-change-in-production")

func NewAuthHandler(db *sql.DB, logger *zap.Logger) *AuthHandler {
	return &AuthHandler{
		db:     db,
		logger: logger,
	}
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req models.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Use username if provided, otherwise use name
	name := req.Name
	if name == "" && req.Username != "" {
		name = req.Username
	}
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name or username is required"})
		return
	}

	// Check if user already exists
	var existingID int
	err := h.db.QueryRow("SELECT id FROM users WHERE email = $1", req.Email).Scan(&existingID)
	if err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "User already exists"})
		return
	} else if err != sql.ErrNoRows {
		traceID := middleware.GetTraceID(c.Request.Context())
		h.logger.Error("Database error", zap.String("trace_id", traceID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		traceID := middleware.GetTraceID(c.Request.Context())
		h.logger.Error("Failed to hash password", zap.String("trace_id", traceID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	// Insert user
	var user models.User
	err = h.db.QueryRow(
		"INSERT INTO users (name, email, password_hash) VALUES ($1, $2, $3) RETURNING id, name, email, created_at",
		name, req.Email, string(hashedPassword),
	).Scan(&user.ID, &user.Name, &user.Email, &user.CreatedAt)
	if err != nil {
		traceID := middleware.GetTraceID(c.Request.Context())
		h.logger.Error("Failed to create user", zap.String("trace_id", traceID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	traceID := middleware.GetTraceID(c.Request.Context())
	h.logger.Info("User registered", zap.String("trace_id", traceID), zap.String("email", req.Email))
	c.JSON(http.StatusCreated, user)
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get user from database
	var user models.User
	err := h.db.QueryRow(
		"SELECT id, name, email, password_hash, created_at FROM users WHERE email = $1",
		req.Email,
	).Scan(&user.ID, &user.Name, &user.Email, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
			return
		}
		traceID := middleware.GetTraceID(c.Request.Context())
		h.logger.Error("Database error", zap.String("trace_id", traceID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Generate JWT token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID,
		"email":   user.Email,
		"exp":     time.Now().Add(time.Hour * 24).Unix(),
	})

	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		traceID := middleware.GetTraceID(c.Request.Context())
		h.logger.Error("Failed to generate token", zap.String("trace_id", traceID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	traceID := middleware.GetTraceID(c.Request.Context())
	h.logger.Info("User logged in", zap.String("trace_id", traceID), zap.String("email", req.Email))
	c.JSON(http.StatusOK, models.LoginResponse{
		Token: tokenString,
		User:  user,
	})
}
