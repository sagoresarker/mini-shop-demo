FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install protoc and dependencies
RUN apk add --no-cache protobuf-dev

COPY go.mod ./
RUN go mod download

# Install Go protobuf plugins
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest


COPY . .

# Generate protobuf files
RUN protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    proto/order.proto proto/product/product.proto

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/order-service ./main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/order-service .

EXPOSE 8082 50051

CMD ["./order-service"]

