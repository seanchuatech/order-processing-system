package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/segmentio/kafka-go"
	"github.com/seanchuatech/order-processing-system/services/order/internal/domain"
)

type KafkaEventPublisher struct {
	writer *kafka.Writer
}

func NewKafkaEventPublisher(brokers []string, topic string) *KafkaEventPublisher {
	return &KafkaEventPublisher{
		writer: &kafka.Writer{
			Addr:     kafka.TCP(brokers...),
			Topic:    topic,
			Balancer: &kafka.LeastBytes{},
		},
	}
}

func (p *KafkaEventPublisher) PublishOrderCreated(ctx context.Context, order *domain.Order) error {
	payload, err := json.Marshal(order)
	if err != nil {
		return fmt.Errorf("failed to marshal order event: %w", err)
	}

	err = p.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(order.ID),
		Value: payload,
	})
	if err != nil {
		return fmt.Errorf("failed to publish order created event to kafka: %w", err)
	}

	return nil
}

func (p *KafkaEventPublisher) Close() error {
	if err := p.writer.Close(); err != nil {
		return fmt.Errorf("failed to close kafka writer: %w", err)
	}
	return nil
}
