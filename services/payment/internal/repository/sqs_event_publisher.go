package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/seanchuatech/order-processing-system/services/payment/internal/domain"
)

type SQSEventPublisher struct {
	client    *sqs.Client
	queueURLs []string
}

func NewSQSEventPublisher(client *sqs.Client, queueURLs []string) *SQSEventPublisher {
	return &SQSEventPublisher{
		client:    client,
		queueURLs: queueURLs,
	}
}

func (p *SQSEventPublisher) PublishPaymentProcessed(ctx context.Context, event *domain.PaymentProcessedEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal payment event: %w", err)
	}

	for _, queueURL := range p.queueURLs {
		if queueURL == "" {
			continue
		}
		_, err = p.client.SendMessage(ctx, &sqs.SendMessageInput{
			QueueUrl:    aws.String(queueURL),
			MessageBody: aws.String(string(payload)),
		})
		if err != nil {
			return fmt.Errorf("failed to publish payment event to SQS queue %s: %w", queueURL, err)
		}
		log.Printf("[Payment Service] Published event to direct local queue: %s", queueURL)
	}

	return nil
}
