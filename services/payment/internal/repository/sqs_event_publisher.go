package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/seanchuatech/order-processing-system/services/payment/internal/domain"
	"github.com/seanchuatech/order-processing-system/services/payment/internal/metricshelper"
	"github.com/seanchuatech/order-processing-system/services/payment/internal/otelhelper"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
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
	tr := otel.Tracer("payment-service")
	ctx, span := tr.Start(ctx, "PublishPaymentProcessed", trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	payload, err := json.Marshal(event)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal payment event: %w", err)
	}

	for _, queueURL := range p.queueURLs {
		if queueURL == "" {
			continue
		}

		start := time.Now()

		messageAttributes := make(map[string]sqstypes.MessageAttributeValue)
		carrier := otelhelper.SQSCarrier(messageAttributes)
		otel.GetTextMapPropagator().Inject(ctx, carrier)

		_, err = p.client.SendMessage(ctx, &sqs.SendMessageInput{
			QueueUrl:          aws.String(queueURL),
			MessageBody:       aws.String(string(payload)),
			MessageAttributes: messageAttributes,
		})
		if err != nil {
			metricshelper.SQSPublishTotal.WithLabelValues(queueURL, "error").Inc()
			metricshelper.SQSPublishDuration.WithLabelValues(queueURL, "error").Observe(time.Since(start).Seconds())
			span.RecordError(err)
			return fmt.Errorf("failed to publish payment event to SQS queue %s: %w", queueURL, err)
		}
		metricshelper.SQSPublishTotal.WithLabelValues(queueURL, "success").Inc()
		metricshelper.SQSPublishDuration.WithLabelValues(queueURL, "success").Observe(time.Since(start).Seconds())
		slog.Info("Published event to direct local queue", "queue_url", queueURL, "order_id", event.OrderID)
	}

	return nil
}
