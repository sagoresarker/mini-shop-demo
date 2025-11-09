package circuitbreaker

import (
	"context"
	"errors"
	"sync"
	"time"
)

type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

type CircuitBreaker struct {
	maxFailures     int
	resetTimeout    time.Duration
	failureCount    int
	lastFailureTime time.Time
	state           State
	mu              sync.RWMutex
}

var (
	ErrCircuitOpen = errors.New("circuit breaker is open")
)

func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
		state:        StateClosed,
	}
}

func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Check if we should transition from Open to HalfOpen
	if cb.state == StateOpen {
		if time.Since(cb.lastFailureTime) > cb.resetTimeout {
			cb.state = StateHalfOpen
			cb.failureCount = 0
		} else {
			return ErrCircuitOpen
		}
	}

	// Execute the function
	err := fn()

	if err != nil {
		cb.failureCount++
		cb.lastFailureTime = time.Now()

		if cb.failureCount >= cb.maxFailures {
			cb.state = StateOpen
		} else if cb.state == StateHalfOpen {
			cb.state = StateOpen
		}
		return err
	}

	// Success - reset if in HalfOpen state
	switch cb.state {
	case StateHalfOpen:
		cb.state = StateClosed
		cb.failureCount = 0
	case StateClosed:
		cb.failureCount = 0
	}

	return nil
}

func (cb *CircuitBreaker) GetState() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}
