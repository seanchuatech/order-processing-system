package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/segmentio/kafka-go"
	"github.com/seanchuatech/order-processing-system/services/notification/internal/domain"
)

type KafkaConsumer struct {
	reader *kafka.Reader
}

func NewKafkaConsumer(brokers []string, topic, groupID string) *KafkaConsumer {
	return &KafkaConsumer{
		reader: kafka.NewReader(kafka.ReaderConfig{
			Brokers:  brokers,
			Topic:    topic,
			GroupID:  groupID,
			MinBytes: 10e3, // 10KB
			MaxBytes: 10e6, // 10MB
		}),
	}
}

func (c *KafkaConsumer) Start(ctx context.Context) error {
	log.Println("Notification service consumer starting...")
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			msg, err := c.reader.ReadMessage(ctx)
			if err != nil {
				// If context is canceled, ReadMessage will return an error, which we should handle gracefully
				select {
				case <-ctx.Done():
					return nil
				default:
					log.Printf("Error reading message from Kafka: %v", err)
					continue
				}
			}

			var order domain.Order
			if err := json.Unmarshal(msg.Value, &order); err != nil {
				log.Printf("Error unmarshaling order payload (Key: %s): %v", string(msg.Key), err)
				continue
			}

			log.Printf("[Notification Service] SUCCESS: Notification dispatched for Order ID: %s | Customer ID: %s | Total: $%.2f",
				order.ID, order.CustomerID, order.TotalPrice)
		}
	}
}

func (c *KafkaConsumer) Close() error {
	if err := c.reader.Close(); err != nil {
		return fmt.Errorf("failed to close kafka reader: %w", err)
	}
	return nil
}
