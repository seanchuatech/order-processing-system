package repository

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
)

type PostgresInventoryRepository struct {
	db *sql.DB
}

func NewPostgresInventoryRepository(db *sql.DB) *PostgresInventoryRepository {
	return &PostgresInventoryRepository{db: db}
}

func (r *PostgresInventoryRepository) DeductStock(ctx context.Context, productID string, quantity int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Check current stock first
	var currentQty int
	err = tx.QueryRowContext(ctx, "SELECT quantity FROM inventory WHERE product_id = $1 FOR UPDATE", productID).Scan(&currentQty)
	if err == sql.ErrNoRows {
		// Create item if not exists with a default stock of 100 first, then deduct
		slog.Info("Product not found in stock, initializing with default 100 items", "product_id", productID)
		_, err = tx.ExecContext(ctx, "INSERT INTO inventory (product_id, quantity) VALUES ($1, $2)", productID, 100)
		if err != nil {
			return fmt.Errorf("failed to initialize stock for %s: %w", productID, err)
		}
		currentQty = 100
	} else if err != nil {
		return fmt.Errorf("failed to query stock for %s: %w", productID, err)
	}

	if currentQty < quantity {
		slog.Warn("Insufficient stock. Deducting to 0", "product_id", productID, "current", currentQty, "requested", quantity)
		_, err = tx.ExecContext(ctx, "UPDATE inventory SET quantity = 0 WHERE product_id = $1", productID)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE inventory SET quantity = quantity - $1 WHERE product_id = $2", quantity, productID)
	}

	if err != nil {
		return fmt.Errorf("failed to update stock for %s: %w", productID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	slog.Info("Stock successfully deducted", "product_id", productID, "deducted_quantity", quantity)
	return nil
}
