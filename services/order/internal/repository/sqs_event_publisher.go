package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/seanchuatech/order-processing-system/services/order/internal/domain"
	"github.com/seanchuatech/order-processing-system/services/order/internal/metricshelper"
	"github.com/seanchuatech/order-processing-system/services/order/internal/otelhelper"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
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
	start := time.Now()
	status := "success"
	defer func() {
		metricshelper.SQSPublishTotal.WithLabelValues(p.queueURL, status).Inc()
		metricshelper.SQSPublishDuration.WithLabelValues(p.queueURL, status).Observe(time.Since(start).Seconds())
	}()

	tr := otel.Tracer("order-service")
	ctx, span := tr.Start(ctx, "PublishOrderCreated", trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	payload, err := json.Marshal(order)
	if err != nil {
		status = "error"
		span.RecordError(err)
		return fmt.Errorf("failed to marshal order event: %w", err)
	}

	messageAttributes := make(map[string]sqstypes.MessageAttributeValue)
	carrier := otelhelper.SQSCarrier(messageAttributes)
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	_, err = p.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:          aws.String(p.queueURL),
		MessageBody:       aws.String(string(payload)),
		MessageAttributes: messageAttributes,
	})
	if err != nil {
		status = "error"
		span.RecordError(err)
		return fmt.Errorf("failed to publish order created event to SQS: %w", err)
	}

	return nil
}

func (p *SQSEventPublisher) Close() error {
	return nil
}
