#!/bin/bash

# Complete System Test Script
# Tests the entire microservices system end-to-end

set -e

echo "=========================================="
echo "  Microservices System Test Suite"
echo "=========================================="
echo ""

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Check if services are running
check_service() {
    local service=$1
    local port=$2
    if curl -s -f "http://localhost:${port}/health" > /dev/null; then
        echo -e "${GREEN}✓${NC} ${service} is healthy"
        return 0
    else
        echo -e "${RED}✗${NC} ${service} is not responding"
        return 1
    fi
}

echo -e "${BLUE}Step 1: Checking Service Health${NC}"
echo "----------------------------------------"
check_service "User Service" 8080
check_service "Product Service" 8081
check_service "Order Service" 8082
check_service "Payment Service" 8083
check_service "Notification Service" 8084
echo ""

echo -e "${BLUE}Step 2: User Registration${NC}"
echo "----------------------------------------"
echo "Registering new user..."
REGISTER_RESPONSE=$(curl -s -X POST http://localhost:8080/register \
  -H "Content-Type: application/json" \
  -d '{
    "username": "testuser",
    "email": "test@example.com",
    "password": "password123"
  }')

if echo "$REGISTER_RESPONSE" | jq -e '.user_id' > /dev/null 2>&1; then
    USER_ID=$(echo "$REGISTER_RESPONSE" | jq -r '.user_id')
    echo -e "${GREEN}✓${NC} User registered successfully (ID: $USER_ID)"
    echo "Response: $REGISTER_RESPONSE" | jq
else
    echo -e "${YELLOW}⚠${NC} User might already exist or error occurred"
    echo "Response: $REGISTER_RESPONSE"
fi
echo ""

echo -e "${BLUE}Step 3: User Login${NC}"
echo "----------------------------------------"
echo "Logging in..."
LOGIN_RESPONSE=$(curl -s -X POST http://localhost:8080/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "test@example.com",
    "password": "password123"
  }')

TOKEN=$(echo "$LOGIN_RESPONSE" | jq -r '.token // empty')
if [ -n "$TOKEN" ] && [ "$TOKEN" != "null" ]; then
    echo -e "${GREEN}✓${NC} Login successful"
    echo "Token: ${TOKEN:0:50}..."
    export JWT_TOKEN="$TOKEN"
else
    echo -e "${RED}✗${NC} Login failed"
    echo "Response: $LOGIN_RESPONSE"
    exit 1
fi
echo ""

echo -e "${BLUE}Step 4: Get User Profile (Protected)${NC}"
echo "----------------------------------------"
PROFILE_RESPONSE=$(curl -s -X GET http://localhost:8080/profile \
  -H "Authorization: Bearer $TOKEN")
echo "$PROFILE_RESPONSE" | jq
echo ""

echo -e "${BLUE}Step 5: Create Products${NC}"
echo "----------------------------------------"
echo "Creating Product 1: Laptop..."
PRODUCT1_RESPONSE=$(curl -s -X POST http://localhost:8081/products \
  -H "Content-Type: application/json" \
  -d '{
    "name": "MacBook Pro",
    "price": 1999.99,
    "stock": 10
  }')
PRODUCT1_ID=$(echo "$PRODUCT1_RESPONSE" | jq -r '.id')
echo -e "${GREEN}✓${NC} Product 1 created (ID: $PRODUCT1_ID)"
echo "$PRODUCT1_RESPONSE" | jq

echo "Creating Product 2: Mouse..."
PRODUCT2_RESPONSE=$(curl -s -X POST http://localhost:8081/products \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Wireless Mouse",
    "price": 29.99,
    "stock": 50
  }')
PRODUCT2_ID=$(echo "$PRODUCT2_RESPONSE" | jq -r '.id')
echo -e "${GREEN}✓${NC} Product 2 created (ID: $PRODUCT2_ID)"
echo "$PRODUCT2_RESPONSE" | jq
echo ""

echo -e "${BLUE}Step 6: List All Products${NC}"
echo "----------------------------------------"
curl -s http://localhost:8081/products | jq
echo ""

echo -e "${BLUE}Step 7: Get Product by ID${NC}"
echo "----------------------------------------"
echo "Getting Product ID: $PRODUCT1_ID"
curl -s "http://localhost:8081/products/$PRODUCT1_ID" | jq
echo ""

echo -e "${BLUE}Step 8: Create Order${NC}"
echo "----------------------------------------"
echo "Creating order for Product ID: $PRODUCT1_ID, Quantity: 2..."
ORDER_RESPONSE=$(curl -s -X POST http://localhost:8082/orders \
  -H "Content-Type: application/json" \
  -d "{
    \"user_id\": $USER_ID,
    \"product_id\": $PRODUCT1_ID,
    \"quantity\": 2
  }")
ORDER_ID=$(echo "$ORDER_RESPONSE" | jq -r '.id')
echo -e "${GREEN}✓${NC} Order created (ID: $ORDER_ID)"
echo "$ORDER_RESPONSE" | jq
echo ""

echo -e "${BLUE}Step 9: Wait for Payment Processing${NC}"
echo "----------------------------------------"
echo "Waiting 5 seconds for payment service to process the order..."
sleep 5
echo -e "${GREEN}✓${NC} Wait complete"
echo ""

echo -e "${BLUE}Step 10: Check Order Status${NC}"
echo "----------------------------------------"
echo "Checking order ID: $ORDER_ID"
ORDER_STATUS=$(curl -s "http://localhost:8082/orders/$ORDER_ID")
echo "$ORDER_STATUS" | jq
ORDER_STATUS_VALUE=$(echo "$ORDER_STATUS" | jq -r '.status')
echo ""
if [ "$ORDER_STATUS_VALUE" = "paid" ]; then
    echo -e "${GREEN}✓${NC} Order payment successful! Status: $ORDER_STATUS_VALUE"
elif [ "$ORDER_STATUS_VALUE" = "failed" ]; then
    echo -e "${YELLOW}⚠${NC} Order payment failed. Status: $ORDER_STATUS_VALUE"
else
    echo -e "${YELLOW}⚠${NC} Order still processing. Status: $ORDER_STATUS_VALUE"
fi
echo ""

echo -e "${BLUE}Step 11: Test Product Availability Check${NC}"
echo "----------------------------------------"
echo "Testing with insufficient stock (quantity: 100)..."
ORDER_FAIL_RESPONSE=$(curl -s -X POST http://localhost:8082/orders \
  -H "Content-Type: application/json" \
  -d "{
    \"user_id\": $USER_ID,
    \"product_id\": $PRODUCT1_ID,
    \"quantity\": 100
  }")
echo "$ORDER_FAIL_RESPONSE" | jq
echo ""

echo -e "${BLUE}Step 12: Check Metrics Endpoints${NC}"
echo "----------------------------------------"
echo "User Service metrics (first 10 lines):"
curl -s http://localhost:8080/metrics | head -10
echo ""
echo "Product Service metrics (first 10 lines):"
curl -s http://localhost:8081/metrics | head -10
echo ""
echo "Order Service metrics (first 10 lines):"
curl -s http://localhost:8082/metrics | head -10
echo ""
echo "Payment Service metrics (first 10 lines):"
curl -s http://localhost:8083/metrics | head -10
echo ""
echo "Notification Service metrics (first 10 lines):"
curl -s http://localhost:8084/metrics | head -10
echo ""

echo -e "${BLUE}Step 13: Final Health Check${NC}"
echo "----------------------------------------"
check_service "User Service" 8080
check_service "Product Service" 8081
check_service "Order Service" 8082
check_service "Payment Service" 8083
check_service "Notification Service" 8084
echo ""

echo "=========================================="
echo -e "${GREEN}Test Suite Complete!${NC}"
echo "=========================================="
echo ""
echo "Access Points:"
echo "  - Prometheus: http://localhost:9090"
echo "  - Grafana: http://localhost:3000 (admin/admin)"
echo "  - Jaeger: http://localhost:16686"
echo "  - Loki: http://localhost:3100"
echo ""

