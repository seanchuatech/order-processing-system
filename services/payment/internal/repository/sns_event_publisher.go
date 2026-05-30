package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/seanchuatech/order-processing-system/services/payment/internal/domain"
)

type SNSEventPublisher struct {
	client   *sns.Client
	topicARN string
}

func NewSNSEventPublisher(client *sns.Client, topicARN string) *SNSEventPublisher {
	return &SNSEventPublisher{
		client:   client,
		topicARN: topicARN,
	}
}

func (p *SNSEventPublisher) PublishPaymentProcessed(ctx context.Context, event *domain.PaymentProcessedEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal payment event: %w", err)
	}

	_, err = p.client.Publish(ctx, &sns.PublishInput{
		TopicArn: aws.String(p.topicARN),
		Message:  aws.String(string(payload)),
	})
	if err != nil {
		return fmt.Errorf("failed to publish payment event to SNS: %w", err)
	}

	return nil
}
