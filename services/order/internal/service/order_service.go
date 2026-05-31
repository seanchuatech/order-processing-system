package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/seanchuatech/order-processing-system/services/order/internal/domain"
	"github.com/seanchuatech/order-processing-system/services/order/internal/repository"
)

type OrderService struct {
	orderRepo repository.OrderRepository
}

func NewOrderService(orderRepo repository.OrderRepository) *OrderService {
	return &OrderService{
		orderRepo: orderRepo,
	}
}

func (s *OrderService) CreateOrder(ctx context.Context, customerID string, items []domain.OrderItem) (*domain.Order, error) {
	if customerID == "" {
		return nil, errors.New("customer ID is required")
	}
	if len(items) == 0 {
		return nil, errors.New("order must contain at least one item")
	}

	var totalPriceCents int64
	for _, item := range items {
		if item.ProductID == "" {
			return nil, errors.New("product ID is required for all items")
		}
		if item.Quantity <= 0 {
			return nil, errors.New("quantity must be greater than zero")
		}
		if item.PriceCents < 0 {
			return nil, errors.New("price cannot be negative")
		}
		totalPriceCents += int64(item.Quantity) * item.PriceCents
	}

	order := &domain.Order{
		ID:              uuid.New().String(),
		CustomerID:      customerID,
		Items:           items,
		TotalPriceCents: totalPriceCents,
		Status:          "PENDING",
		CreatedAt:       time.Now().UTC(),
	}

	// 1. Save to Database
	if err := s.orderRepo.Create(ctx, order); err != nil {
		return nil, fmt.Errorf("failed to save order: %w", err)
	}

	return order, nil
}

func (s *OrderService) GetOrder(ctx context.Context, id string) (*domain.Order, error) {
	if id == "" {
		return nil, errors.New("order ID is required")
	}
	return s.orderRepo.GetByID(ctx, id)
}
