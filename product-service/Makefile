.PHONY: proto build run clean install-deps

# Protobuf generation
proto:
	@echo "Generating protobuf files..."
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/*.proto
	@echo "Protobuf files generated successfully!"

# Install protoc dependencies
install-deps:
	@echo "Installing protoc plugins..."
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@echo "Dependencies installed!"

# Build the service
build: proto
	@echo "Building product-service..."
	CGO_ENABLED=0 GOOS=linux go build -o bin/product-service ./main.go
	@echo "Build complete!"

# Run the service locally
run:
	@echo "Running product-service..."
	go run main.go

# Clean generated files
clean:
	@echo "Cleaning generated files..."
	rm -rf bin/
	find . -name "*.pb.go" -delete
	@echo "Clean complete!"

# Generate proto and tidy dependencies
setup: install-deps proto
	@echo "Setting up project..."
	go mod tidy
	@echo "Setup complete!"

