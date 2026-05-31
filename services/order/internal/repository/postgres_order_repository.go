package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/seanchuatech/order-processing-system/services/order/internal/domain"
)

type PostgresOrderRepository struct {
	db *sql.DB
}

func NewPostgresOrderRepository(db *sql.DB) *PostgresOrderRepository {
	return &PostgresOrderRepository{db: db}
}

func (r *PostgresOrderRepository) Create(ctx context.Context, order *domain.Order) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback() // Rollback is a no-op if committed successfully
	}()

	orderQuery := `
		INSERT INTO orders (id, customer_id, total_price_cents, status, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err = tx.ExecContext(ctx, orderQuery, order.ID, order.CustomerID, order.TotalPriceCents, order.Status, order.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to insert order: %w", err)
	}

	itemQuery := `
		INSERT INTO order_items (order_id, product_id, quantity, price_cents)
		VALUES ($1, $2, $3, $4)
	`
	for _, item := range order.Items {
		_, err = tx.ExecContext(ctx, itemQuery, order.ID, item.ProductID, item.Quantity, item.PriceCents)
		if err != nil {
			return fmt.Errorf("failed to insert order item (%s): %w", item.ProductID, err)
		}
	}

	// Insert into outbox table
	payload, err := json.Marshal(order)
	if err != nil {
		return fmt.Errorf("failed to marshal order outbox payload: %w", err)
	}

	outboxQuery := `
		INSERT INTO outbox (id, aggregate_id, payload, status, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	outboxID := uuid.New().String()
	_, err = tx.ExecContext(ctx, outboxQuery, outboxID, order.ID, payload, "pending", order.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to insert outbox record: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func (r *PostgresOrderRepository) GetByID(ctx context.Context, id string) (*domain.Order, error) {
	orderQuery := `
		SELECT id, customer_id, total_price_cents, status, created_at
		FROM orders
		WHERE id = $1
	`
	var order domain.Order
	err := r.db.QueryRowContext(ctx, orderQuery, id).Scan(
		&order.ID, &order.CustomerID, &order.TotalPriceCents, &order.Status, &order.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("order not found: %s", id)
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch order: %w", err)
	}

	itemsQuery := `
		SELECT product_id, quantity, price_cents
		FROM order_items
		WHERE order_id = $1
	`
	rows, err := r.db.QueryContext(ctx, itemsQuery, id)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch order items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var item domain.OrderItem
		if err := rows.Scan(&item.ProductID, &item.Quantity, &item.PriceCents); err != nil {
			return nil, fmt.Errorf("failed to scan order item: %w", err)
		}
		order.Items = append(order.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading order items rows: %w", err)
	}

	return &order, nil
}
