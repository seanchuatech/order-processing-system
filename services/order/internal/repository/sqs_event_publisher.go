package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/seanchuatech/order-processing-system/services/order/internal/domain"
)

type SQSEventPublisher struct {
	client   *sqs.Client
	queueURL string
}

func NewSQSEventPublisher(client *sqs.Client, queueURL string) *SQSEventPublisher {
	return &SQSEventPublisher{
		client:   client,
		queueURL: queueURL,
	}
}

func (p *SQSEventPublisher) PublishOrderCreated(ctx context.Context, order *domain.Order) error {
	payload, err := json.Marshal(order)
	if err != nil {
		return fmt.Errorf("failed to marshal order event: %w", err)
	}

	_, err = p.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(p.queueURL),
		MessageBody: aws.String(string(payload)),
	})
	if err != nil {
		return fmt.Errorf("failed to publish order created event to SQS: %w", err)
	}

	return nil
}

func (p *SQSEventPublisher) Close() error {
	return nil
}
