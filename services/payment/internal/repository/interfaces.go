package repository

import (
	"context"

	"github.com/seanchuatech/order-processing-system/services/payment/internal/domain"
)

type EventPublisher interface {
	PublishPaymentProcessed(ctx context.Context, event *domain.PaymentProcessedEvent) error
}
