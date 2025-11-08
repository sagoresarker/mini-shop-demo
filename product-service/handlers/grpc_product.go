package handlers

import (
	"context"
	"database/sql"

	"product-svc/models"
	product "product-svc/proto"

	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
)

type ProductService struct {
	product.UnimplementedProductServiceServer
	db     *sql.DB
	logger *zap.Logger
}

func NewProductService(db *sql.DB, logger *zap.Logger) *ProductService {
	return &ProductService{
		db:     db,
		logger: logger,
	}
}

func (s *ProductService) GetProduct(ctx context.Context, req *product.GetProductRequest) (*product.GetProductResponse, error) {
	ctx, span := otel.Tracer("product-service").Start(ctx, "GetProduct_gRPC")
	defer span.End()

	var p models.Product
	err := s.db.QueryRowContext(ctx,
		"SELECT id, name, price, stock FROM products WHERE id = $1",
		req.ProductId,
	).Scan(&p.ID, &p.Name, &p.Price, &p.Stock)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		return nil, err
	}

	return &product.GetProductResponse{
		Id:    int32(p.ID),
		Name:  p.Name,
		Price: float32(p.Price),
		Stock: int32(p.Stock),
	}, nil
}

func (s *ProductService) CheckAvailability(ctx context.Context, req *product.CheckAvailabilityRequest) (*product.CheckAvailabilityResponse, error) {
	ctx, span := otel.Tracer("product-service").Start(ctx, "CheckAvailability_gRPC")
	defer span.End()

	var stock int
	err := s.db.QueryRowContext(ctx,
		"SELECT stock FROM products WHERE id = $1",
		req.ProductId,
	).Scan(&stock)

	if err != nil {
		if err == sql.ErrNoRows {
			return &product.CheckAvailabilityResponse{
				Available: false,
				Stock:     0,
			}, nil
		}
		return nil, err
	}

	available := stock >= int(req.Quantity)
	return &product.CheckAvailabilityResponse{
		Available: available,
		Stock:     int32(stock),
	}, nil
}
