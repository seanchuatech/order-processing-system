package repository

import (
	"context"

	"github.com/seanchuatech/order-processing-system/services/order/internal/domain"
)

type OrderRepository interface {
	Create(ctx context.Context, order *domain.Order) error
	GetByID(ctx context.Context, id string) (*domain.Order, error)
}

type EventPublisher interface {
	PublishOrderCreated(ctx context.Context, order *domain.Order) error
	Close() error
}
