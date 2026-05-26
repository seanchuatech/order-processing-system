package domain

type Order struct {
	ID         string  `json:"id"`
	CustomerID string  `json:"customer_id"`
	TotalPrice float64 `json:"total_price"`
}
