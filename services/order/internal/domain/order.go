package domain

import (
	"time"
)

type OrderItem struct {
	ProductID  string `json:"product_id"`
	Quantity   int    `json:"quantity"`
	PriceCents int64  `json:"price_cents"`
}

type Order struct {
	ID              string      `json:"id"`
	CustomerID      string      `json:"customer_id"`
	Items           []OrderItem `json:"items"`
	TotalPriceCents int64       `json:"total_price_cents"`
	Status          string      `json:"status"`
	CreatedAt       time.Time   `json:"created_at"`
}
