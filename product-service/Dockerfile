FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install protoc
RUN apk add --no-cache protobuf-dev

COPY go.mod go.sum ./
RUN go mod download

# Install Go protobuf plugins
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

COPY . .

# Generate protobuf files
RUN protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    proto/*.proto

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/product-service ./main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/product-service .

EXPOSE 8081 50052

CMD ["./product-service"]