package consumer

import (
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/seanchuatech/order-processing-system/services/payment/internal/domain"
	"github.com/seanchuatech/order-processing-system/services/payment/internal/repository"
)

type SQSConsumer struct {
	client    *sqs.Client
	queueURL  string
	publisher repository.EventPublisher
}

func NewSQSConsumer(client *sqs.Client, queueURL string, publisher repository.EventPublisher) *SQSConsumer {
	return &SQSConsumer{
		client:    client,
		queueURL:  queueURL,
		publisher: publisher,
	}
}

func (c *SQSConsumer) Start(ctx context.Context) error {
	log.Println("Payment service SQS consumer starting...")
	// Seed random for simulator
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

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
				var order domain.Order
				if err := json.Unmarshal([]byte(*msg.Body), &order); err != nil {
					log.Printf("Error unmarshaling order payload: %v", err)
					continue
				}

				log.Printf("[Payment Service] Processing payment for Order ID: %s | Amount: $%.2f", order.ID, order.TotalPrice)

				// Simulate payment processing (80% Success, 20% Fail)
				status := "SUCCESS"
				if r.Float32() < 0.2 {
					status = "FAILED"
				}

				// Map order items to event items
				eventItems := make([]domain.EventItem, len(order.Items))
				for i, item := range order.Items {
					eventItems[i] = domain.EventItem(item)
				}

				event := &domain.PaymentProcessedEvent{
					OrderID:    order.ID,
					CustomerID: order.CustomerID,
					Amount:     order.TotalPrice,
					Status:     status,
					Items:      eventItems,
				}

				// Publish the event
				if err := c.publisher.PublishPaymentProcessed(ctx, event); err != nil {
					log.Printf("Error publishing payment processed event: %v", err)
					continue
				}

				log.Printf("[Payment Service] Payment status for Order ID %s is %s", order.ID, status)

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

func (c *SQSConsumer) Close() error {
	return nil
}
