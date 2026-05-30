package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/seanchuatech/order-processing-system/services/payment/internal/domain"
	"github.com/seanchuatech/order-processing-system/services/payment/internal/metricshelper"
	"github.com/seanchuatech/order-processing-system/services/payment/internal/otelhelper"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
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
	start := time.Now()
	status := "success"
	defer func() {
		metricshelper.SNSPublishTotal.WithLabelValues(p.topicARN, status).Inc()
		metricshelper.SNSPublishDuration.WithLabelValues(p.topicARN, status).Observe(time.Since(start).Seconds())
	}()

	tr := otel.Tracer("payment-service")
	ctx, span := tr.Start(ctx, "PublishPaymentProcessed", trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	payload, err := json.Marshal(event)
	if err != nil {
		status = "error"
		span.RecordError(err)
		return fmt.Errorf("failed to marshal payment event: %w", err)
	}

	messageAttributes := make(map[string]snstypes.MessageAttributeValue)
	carrier := otelhelper.SNSCarrier(messageAttributes)
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	_, err = p.client.Publish(ctx, &sns.PublishInput{
		TopicArn:          aws.String(p.topicARN),
		Message:           aws.String(string(payload)),
		MessageAttributes: messageAttributes,
	})
	if err != nil {
		status = "error"
		span.RecordError(err)
		return fmt.Errorf("failed to publish payment event to SNS: %w", err)
	}

	return nil
}
