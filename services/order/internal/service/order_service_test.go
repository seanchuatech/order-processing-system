package service

import (
	"context"
	"errors"
	"testing"

	"github.com/seanchuatech/order-processing-system/services/order/internal/domain"
)

type mockOrderRepo struct {
	createFunc func(ctx context.Context, order *domain.Order) error
	getFunc    func(ctx context.Context, id string) (*domain.Order, error)
}

func (m *mockOrderRepo) Create(ctx context.Context, order *domain.Order) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, order)
	}
	return nil
}

func (m *mockOrderRepo) GetByID(ctx context.Context, id string) (*domain.Order, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return nil, nil
}

func TestCreateOrder(t *testing.T) {
	tests := []struct {
		name       string
		customerID string
		items      []domain.OrderItem
		mockRepo   func() *mockOrderRepo
		expectErr  bool
		expectSum  int64
	}{
		{
			name:       "Happy Path",
			customerID: "cust-1",
			items: []domain.OrderItem{
				{ProductID: "prod-1", Quantity: 2, PriceCents: 1000},
				{ProductID: "prod-2", Quantity: 1, PriceCents: 1550},
			},
			mockRepo: func() *mockOrderRepo {
				return &mockOrderRepo{
					createFunc: func(ctx context.Context, order *domain.Order) error {
						return nil
					},
				}
			},
			expectErr: false,
			expectSum: 3550,
		},
		{
			name:       "Empty Customer ID",
			customerID: "",
			items: []domain.OrderItem{
				{ProductID: "prod-1", Quantity: 1, PriceCents: 1000},
			},
			mockRepo:  func() *mockOrderRepo { return &mockOrderRepo{} },
			expectErr: true,
		},
		{
			name:       "Empty Items List",
			customerID: "cust-1",
			items:      []domain.OrderItem{},
			mockRepo:   func() *mockOrderRepo { return &mockOrderRepo{} },
			expectErr:  true,
		},
		{
			name:       "Negative Price",
			customerID: "cust-1",
			items: []domain.OrderItem{
				{ProductID: "prod-1", Quantity: 1, PriceCents: -500},
			},
			mockRepo:  func() *mockOrderRepo { return &mockOrderRepo{} },
			expectErr: true,
		},
		{
			name:       "Database Save Error",
			customerID: "cust-1",
			items: []domain.OrderItem{
				{ProductID: "prod-1", Quantity: 1, PriceCents: 1000},
			},
			mockRepo: func() *mockOrderRepo {
				return &mockOrderRepo{
					createFunc: func(ctx context.Context, order *domain.Order) error {
						return errors.New("db write error")
					},
				}
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewOrderService(tt.mockRepo())
			order, err := svc.CreateOrder(context.Background(), tt.customerID, tt.items)

			if (err != nil) != tt.expectErr {
				t.Fatalf("expected error: %v, got: %v", tt.expectErr, err)
			}

			if !tt.expectErr {
				if order.TotalPriceCents != tt.expectSum {
					t.Errorf("expected total price: %d, got: %d", tt.expectSum, order.TotalPriceCents)
				}
				if order.Status != "PENDING" {
					t.Errorf("expected status: PENDING, got: %s", order.Status)
				}
			}
		})
	}
}
