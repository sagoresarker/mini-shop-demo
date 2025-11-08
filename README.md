# Microservices Workshop - Go Implementation

This project contains a complete microservices-based e-commerce system built with Go:

1. **User Service** - REST API with Postgres, JWT authentication, bcrypt password hashing
2. **Product Service** - REST + gRPC API with Postgres, Redis caching, Circuit Breaker
3. **Order Service** - REST + gRPC API with Kafka, Saga pattern
4. **Payment Service** - Kafka consumer, payment processing simulation
5. **Notification Service** - Kafka consumer, event-driven notifications

## Complete System Architecture

- **5 Microservices** communicating via REST, gRPC, and Kafka
- **Event-Driven Architecture** with Kafka for asynchronous messaging
- **Saga Pattern** for distributed transaction management
- **Full Observability Stack**: Prometheus, Grafana, Jaeger, Loki
- **Resilience Patterns**: Circuit breakers, retry logic, caching

## Prerequisites

- Docker and Docker Compose
- Go 1.23+ (for local development)
- Protocol Buffers compiler (protoc)

## Quick Start

1. **Start all services with Docker Compose:**
   ```bash
   docker-compose up -d
   ```

2. **Wait for services to be healthy:**
   ```bash
   docker-compose ps
   ```

3. **Access services:**
   - User Service: http://localhost:8080
   - Product Service: http://localhost:8081 (REST) + :50052 (gRPC)
   - Order Service: http://localhost:8082 (REST) + :50051 (gRPC)
   - Payment Service: http://localhost:8083
   - Notification Service: http://localhost:8084
   - Prometheus: http://localhost:9090
   - Grafana: http://localhost:3000 (admin/admin)
   - Jaeger UI: http://localhost:16686
   - Loki: http://localhost:3100

## Service Endpoints

### User Service (Port 8080)
- `POST /register` - Register a new user
- `POST /login` - Login and get JWT token
- `GET /profile` - Get user profile (requires JWT)
- `GET /health` - Health check
- `GET /metrics` - Prometheus metrics

### Product Service (Port 8081)
- `GET /products` - List all products
- `GET /products/:id` - Get product by ID (cached)
- `POST /products` - Create a product
- `PUT /products/:id` - Update a product
- `DELETE /products/:id` - Delete a product
- `GET /health` - Health check
- `GET /metrics` - Prometheus metrics
- gRPC on port 50052

### Order Service (Port 8082)
- `POST /orders` - Create an order
- `GET /orders/:id` - Get order by ID
- `GET /health` - Health check
- `GET /metrics` - Prometheus metrics
- gRPC on port 50051

### Payment Service (Port 8083)
- `GET /health` - Health check
- `GET /metrics` - Prometheus metrics
- Consumes `order_created` events from Kafka
- Publishes `payment_success` or `payment_failed` events

### Notification Service (Port 8084)
- `GET /health` - Health check
- `GET /metrics` - Prometheus metrics
- Consumes all events from Kafka (order_created, payment_success, payment_failed)
- Sends notifications with retry logic

## Local Development

For each service, you need to:

1. **Install dependencies:**
   ```bash
   cd user-service  # or product-service, order-service
   go mod tidy
   ```

2. **For services with gRPC (Product and Order):**
   ```bash
   # Install protoc plugins
   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
   go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

   # Generate protobuf files
   protoc --go_out=. --go_opt=paths=source_relative \
          --go-grpc_out=. --go-grpc_opt=paths=source_relative \
          proto/*.proto
   ```

3. **Run the service:**
   ```bash
   go run main.go
   ```

## Environment Variables

Each service uses environment variables for configuration. See `docker-compose.yml` for examples.

## Architecture

- **User Service**: Handles user registration, authentication with JWT tokens
- **Product Service**: Manages product catalog with Redis caching and circuit breaker, exposes gRPC for other services
- **Order Service**: Processes orders, validates product availability via gRPC, publishes events to Kafka, implements Saga pattern
- **Payment Service**: Consumes order events, simulates payment processing, publishes payment results
- **Notification Service**: Consumes all events, sends notifications with retry logic

## Event Flow

1. **Order Created** → Order Service publishes `order_created` event to Kafka
2. **Payment Processing** → Payment Service consumes event, processes payment, publishes `payment_success` or `payment_failed`
3. **Notifications** → Notification Service consumes all events and sends notifications
4. **Order Update** → Order Service consumes payment events and updates order status (Saga pattern)

## Quick Testing

### Automated Test Script
```bash
./test-system.sh
```

## Features Implemented

**5 Microservices** with independent databases
**REST APIs** with Gin framework
**gRPC** for inter-service communication
**Kafka** event streaming for asynchronous messaging
**Saga Pattern** for distributed transaction management
**PostgreSQL** persistence (one database per service)
**Redis** caching for product service
**JWT authentication** with bcrypt password hashing
**Circuit Breaker** pattern for resilience
**Retry Logic** in notification service
**OpenTelemetry** tracing to Jaeger
**Prometheus** metrics collection
**Grafana** dashboards for visualization
**Loki + Promtail** for log aggregation
**Structured logging** with Zap
**Docker and Docker Compose** setup
**Health check** endpoints for all services
**Complete observability** stack

