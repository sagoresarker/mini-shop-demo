# Quick Test Guide - curl Commands

## Prerequisites
- All services running via `docker-compose up`
- `jq` installed for JSON formatting (optional but recommended)

## Quick Test Sequence

### 1. Health Checks
```bash
# Check all services are healthy
curl http://localhost:8080/health  # User Service
curl http://localhost:8081/health  # Product Service
curl http://localhost:8082/health  # Order Service
curl http://localhost:8083/health  # Payment Service
curl http://localhost:8084/health  # Notification Service
```

### 2. User Registration
```bash
curl -X POST http://localhost:8080/register \
  -H "Content-Type: application/json" \
  -d '{
    "username": "testuser1",
    "email": "test@example1.com",
    "password": "password123"
  }'
```

### 3. User Login (Get JWT Token)
```bash
curl -X POST http://localhost:8080/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "test@example.com",
    "password": "password123"
  }'
```

**Save the token**:
```bash
export JWT_TOKEN="<paste_token_here>"
```

### 4. Get User Profile (Protected)
```bash
curl -X GET http://localhost:8080/profile \
  -H "Authorization: Bearer $JWT_TOKEN"
```

### 5. Create Products
```bash
# Product 1
curl -X POST http://localhost:8081/products \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Laptop",
    "price": 999.99,
    "stock": 2
  }'

# Product 2
curl -X POST http://localhost:8081/products \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Mouse",
    "price": 29.99,
    "stock": 50
  }'
```

### 6. List All Products
```bash
curl http://localhost:8081/products
```

### 7. Get Product by ID
```bash
curl http://localhost:8081/products/1
```

### 8. Create Order (Triggers Full Flow)
```bash
curl -X POST http://localhost:8082/orders \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": 1,
    "product_id": 3,
    "quantity": 2
  }'
```

**What happens**:
1. Order Service checks product availability (gRPC)
2. Order created in database
3. `order_created` event published to Kafka
4. Payment Service processes payment
5. Payment Service publishes `payment_success` or `payment_failed`
6. Notification Service sends notifications
7. Order Service updates order status

### 9. Check Order Status (After 5 seconds)
```bash
# Wait a few seconds for payment processing
sleep 5

# Check order status
curl http://localhost:8082/orders/1
```

### 10. View Metrics
```bash
# All services expose Prometheus metrics
curl http://localhost:8080/metrics | grep http_requests_total
curl http://localhost:8081/metrics | grep http_requests_total
curl http://localhost:8082/metrics | grep http_requests_total
curl http://localhost:8083/metrics | grep payment_processed_total
curl http://localhost:8084/metrics | grep notifications_sent_total
```

## Complete Test Script

Run the automated test script:
```bash
./test-system.sh
```

Or run individual commands above manually.

## Expected Event Flow

1. **Order Created** → Kafka topic `order_events`
2. **Payment Service** consumes → Processes payment → Publishes result
3. **Notification Service** consumes → Sends notifications
4. **Order Service** consumes payment events → Updates order status

## Monitoring

- **Prometheus**: http://localhost:9090
- **Grafana**: http://localhost:3000 (admin/admin)
- **Jaeger**: http://localhost:16686
- **Loki**: http://localhost:3100

## Troubleshooting

If services don't respond:
```bash
# Check Docker containers
docker-compose ps

# View logs
docker-compose logs <service-name>

# Restart a service
docker-compose restart <service-name>
```

