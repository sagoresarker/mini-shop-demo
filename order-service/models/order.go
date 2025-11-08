package models

import "time"

type OrderStatus string

const (
	OrderStatusPending   OrderStatus = "pending"
	OrderStatusPaid      OrderStatus = "paid"
	OrderStatusFailed    OrderStatus = "failed"
	OrderStatusCancelled OrderStatus = "cancelled"
)

type Order struct {
	ID         int         `json:"id"`
	UserID     int         `json:"user_id"`
	ProductID  int         `json:"product_id"`
	Quantity   int         `json:"quantity"`
	Status     OrderStatus `json:"status"`
	TotalPrice float64     `json:"total_price"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

type CreateOrderRequest struct {
	UserID    int `json:"user_id" binding:"required"`
	ProductID int `json:"product_id" binding:"required"`
	Quantity  int `json:"quantity" binding:"required,gt=0"`
}

type OrderEvent struct {
	OrderID    int         `json:"order_id"`
	UserID     int         `json:"user_id"`
	ProductID  int         `json:"product_id"`
	Quantity   int         `json:"quantity"`
	Status     OrderStatus `json:"status"`
	TotalPrice float64     `json:"total_price"`
	EventType  string      `json:"event_type"` // order_created, order_paid, order_failed
}
