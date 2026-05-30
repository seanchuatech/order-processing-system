package consumer

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/seanchuatech/order-processing-system/services/analytics/internal/domain"
)

type SQSConsumer struct {
	client   *sqs.Client
	queueURL string

	// In-memory metrics tracking
	mu                 sync.RWMutex
	totalRevenue       float64
	successfulPayments int
	failedPayments     int
	totalPayments      int
}

func NewSQSConsumer(client *sqs.Client, queueURL string) *SQSConsumer {
	return &SQSConsumer{
		client:   client,
		queueURL: queueURL,
	}
}

func (c *SQSConsumer) Start(ctx context.Context) error {
	log.Println("Analytics service SQS consumer starting...")
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

				// Update metrics
				c.mu.Lock()
				c.totalPayments++
				if event.Status == "SUCCESS" {
					c.successfulPayments++
					c.totalRevenue += event.Amount
				} else {
					c.failedPayments++
				}
				totalRev := c.totalRevenue
				successCount := c.successfulPayments
				failedCount := c.failedPayments
				totalCount := c.totalPayments
				c.mu.Unlock()

				successRate := 0.0
				if totalCount > 0 {
					successRate = (float64(successCount) / float64(totalCount)) * 100.0
				}

				log.Printf("[Analytics Service] PROCESSED: Order: %s | Status: %s | Amount: $%.2f",
					event.OrderID, event.Status, event.Amount)
				log.Printf("[Analytics Service] METRICS: Total Revenue: $%.2f | Successful: %d | Failed: %d | Success Rate: %.1f%%",
					totalRev, successCount, failedCount, successRate)

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
