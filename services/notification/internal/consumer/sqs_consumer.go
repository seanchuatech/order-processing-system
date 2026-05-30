package consumer

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/seanchuatech/order-processing-system/services/notification/internal/domain"
)

type SQSConsumer struct {
	client   *sqs.Client
	queueURL string
}

func NewSQSConsumer(client *sqs.Client, queueURL string) *SQSConsumer {
	return &SQSConsumer{
		client:   client,
		queueURL: queueURL,
	}
}

func (c *SQSConsumer) Start(ctx context.Context) error {
	log.Println("Notification service SQS consumer starting...")
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

				log.Printf("[Notification Service] SUCCESS: Notification dispatched for Order ID: %s | Customer ID: %s | Total: $%.2f",
					order.ID, order.CustomerID, order.TotalPrice)

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
