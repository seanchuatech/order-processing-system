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
	publisher repository.EventPublisher
}

func NewOrderService(orderRepo repository.OrderRepository, publisher repository.EventPublisher) *OrderService {
	return &OrderService{
		orderRepo: orderRepo,
		publisher: publisher,
	}
}

func (s *OrderService) CreateOrder(ctx context.Context, customerID string, items []domain.OrderItem) (*domain.Order, error) {
	if customerID == "" {
		return nil, errors.New("customer ID is required")
	}
	if len(items) == 0 {
		return nil, errors.New("order must contain at least one item")
	}

	var totalPrice float64
	for _, item := range items {
		if item.ProductID == "" {
			return nil, errors.New("product ID is required for all items")
		}
		if item.Quantity <= 0 {
			return nil, errors.New("quantity must be greater than zero")
		}
		if item.Price < 0 {
			return nil, errors.New("price cannot be negative")
		}
		totalPrice += float64(item.Quantity) * item.Price
	}

	order := &domain.Order{
		ID:         uuid.New().String(),
		CustomerID: customerID,
		Items:      items,
		TotalPrice: totalPrice,
		Status:     "PENDING",
		CreatedAt:  time.Now().UTC(),
	}

	// 1. Save to Database
	if err := s.orderRepo.Create(ctx, order); err != nil {
		return nil, fmt.Errorf("failed to save order: %w", err)
	}

	// 2. Publish Event
	if err := s.publisher.PublishOrderCreated(ctx, order); err != nil {
		// Log error but do not fail the request completely since DB succeeded (or we can return error)
		// For consistency in our local system, let's treat Kafka publish failures as errors
		return nil, fmt.Errorf("failed to publish order event: %w", err)
	}

	return order, nil
}

func (s *OrderService) GetOrder(ctx context.Context, id string) (*domain.Order, error) {
	if id == "" {
		return nil, errors.New("order ID is required")
	}
	return s.orderRepo.GetByID(ctx, id)
}
