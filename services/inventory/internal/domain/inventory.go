package domain

type InventoryItem struct {
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

type EventItem struct {
	ProductID string  `json:"product_id"`
	Quantity  int     `json:"quantity"`
	Price     float64 `json:"price"`
}

type PaymentProcessedEvent struct {
	OrderID    string      `json:"order_id"`
	CustomerID string      `json:"customer_id"`
	Amount     float64     `json:"amount"`
	Status     string      `json:"status"` // "SUCCESS" or "FAILED"
	Items      []EventItem `json:"items"`
}
