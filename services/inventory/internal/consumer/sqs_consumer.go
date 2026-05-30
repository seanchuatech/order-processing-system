package consumer

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/seanchuatech/order-processing-system/services/inventory/internal/domain"
	"github.com/seanchuatech/order-processing-system/services/inventory/internal/repository"
)

type SQSConsumer struct {
	client   *sqs.Client
	queueURL string
	repo     *repository.PostgresInventoryRepository
}

func NewSQSConsumer(client *sqs.Client, queueURL string, repo *repository.PostgresInventoryRepository) *SQSConsumer {
	return &SQSConsumer{
		client:   client,
		queueURL: queueURL,
		repo:     repo,
	}
}

func (c *SQSConsumer) Start(ctx context.Context) error {
	log.Println("Inventory service SQS consumer starting...")
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Poll SQS (Long Polling)
			result, err := c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
				QueueUrl:            aws.String(c.queueURL),
				MaxNumberOfMessages: 10,
				WaitTimeSeconds:     10, // Long polling
			})
			if err != nil {
				select {
				case <-ctx.Done():
					return nil
				default:
					log.Printf("Error receiving messages from SQS: %v", err)
					time.Sleep(2 * time.Second)
					continue
				}
			}

			for _, msg := range result.Messages {
				event, err := parseMessageBody(*msg.Body)
				if err != nil {
					log.Printf("Error parsing message body: %v", err)
					continue
				}

				if event.Status == "SUCCESS" {
					log.Printf("[Inventory Service] Processing SUCCESS payment for Order ID: %s. Deducting stock...", event.OrderID)
					for _, item := range event.Items {
						if err := c.repo.DeductStock(ctx, item.ProductID, item.Quantity); err != nil {
							log.Printf("Error deducting stock for product %s in order %s: %v", item.ProductID, event.OrderID, err)
						}
					}
				} else {
					log.Printf("[Inventory Service] Skipping stock deduction for failed payment of Order ID: %s", event.OrderID)
				}

				// Delete message after successful processing
				_, err = c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
					QueueUrl:      aws.String(c.queueURL),
					ReceiptHandle: msg.ReceiptHandle,
				})
				if err != nil {
					log.Printf("Error deleting message from SQS: %v", err)
				}
			}
		}
	}
}

func parseMessageBody(body string) (*domain.PaymentProcessedEvent, error) {
	// Try parsing as SNS Envelope first
	var envelope struct {
		Message string `json:"Message"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err == nil && envelope.Message != "" {
		// It's an SNS envelope, unmarshal the inner Message
		var event domain.PaymentProcessedEvent
		if err := json.Unmarshal([]byte(envelope.Message), &event); err == nil {
			return &event, nil
		}
	}

	// Fallback/direct unmarshal (local mode)
	var event domain.PaymentProcessedEvent
	if err := json.Unmarshal([]byte(body), &event); err != nil {
		return nil, err
	}
	return &event, nil
}

func (c *SQSConsumer) Close() error {
	return nil
}
