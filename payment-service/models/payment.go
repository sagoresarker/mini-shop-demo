package models

import "time"

type PaymentStatus string

const (
	PaymentStatusPending   PaymentStatus = "pending"
	PaymentStatusSuccess   PaymentStatus = "success"
	PaymentStatusFailed    PaymentStatus = "failed"
	PaymentStatusCancelled PaymentStatus = "cancelled"
)

type Payment struct {
	ID            int           `json:"id"`
	OrderID       int           `json:"order_id"`
	UserID        int           `json:"user_id"`
	Amount        float64       `json:"amount"`
	Status        PaymentStatus `json:"status"`
	TransactionID string        `json:"transaction_id"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

type PaymentEvent struct {
	PaymentID     int           `json:"payment_id"`
	OrderID       int           `json:"order_id"`
	UserID        int           `json:"user_id"`
	Amount        float64       `json:"amount"`
	Status        PaymentStatus `json:"status"`
	EventType     string        `json:"event_type"` // payment_success, payment_failed
	TransactionID string        `json:"transaction_id"`
}
