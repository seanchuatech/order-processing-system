package consumer

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/seanchuatech/order-processing-system/services/payment/internal/domain"
	"github.com/seanchuatech/order-processing-system/services/payment/internal/metricshelper"
	"github.com/seanchuatech/order-processing-system/services/payment/internal/otelhelper"
	"github.com/seanchuatech/order-processing-system/services/payment/internal/repository"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
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
	slog.Info("Payment service SQS consumer starting...")
	// Seed random for simulator
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Poll SQS (Long Polling)
			result, err := c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
				QueueUrl:              aws.String(c.queueURL),
				MaxNumberOfMessages:   10,
				WaitTimeSeconds:       10, // Long polling
				MessageAttributeNames: []string{"All"},
			})
			if err != nil {
				select {
				case <-ctx.Done():
					return nil
				default:
					slog.Error("Error receiving messages from SQS", "error", err)
					time.Sleep(2 * time.Second)
					continue
				}
			}

			for _, msg := range result.Messages {
				start := time.Now()
				status := "success"

				// Extract tracing context from message attributes
				carrier := otelhelper.SQSCarrier(msg.MessageAttributes)
				parentCtx := otel.GetTextMapPropagator().Extract(ctx, carrier)

				tr := otel.Tracer("payment-service")
				msgCtx, span := tr.Start(parentCtx, "ProcessPayment", trace.WithSpanKind(trace.SpanKindConsumer))

				var order domain.Order
				if err := json.Unmarshal([]byte(*msg.Body), &order); err != nil {
					status = "error"
					slog.Error("Error unmarshaling order payload", "error", err)
					span.RecordError(err)
					span.End()
					metricshelper.SQSConsumeTotal.WithLabelValues(c.queueURL, status).Inc()
					metricshelper.SQSConsumeDuration.WithLabelValues(c.queueURL, status).Observe(time.Since(start).Seconds())
					continue
				}

				slog.Info("Processing payment", "order_id", order.ID, "amount", order.TotalPrice)

				// Simulate payment processing (80% Success, 20% Fail)
				statusDecision := "SUCCESS"
				if r.Float32() < 0.2 {
					statusDecision = "FAILED"
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
					Status:     statusDecision,
					Items:      eventItems,
				}

				// Publish the event (using the traced msgCtx!)
				if err := c.publisher.PublishPaymentProcessed(msgCtx, event); err != nil {
					status = "error"
					slog.Error("Error publishing payment processed event", "error", err)
					span.RecordError(err)
					span.End()
					metricshelper.SQSConsumeTotal.WithLabelValues(c.queueURL, status).Inc()
					metricshelper.SQSConsumeDuration.WithLabelValues(c.queueURL, status).Observe(time.Since(start).Seconds())
					continue
				}

				slog.Info("Payment status decided", "order_id", order.ID, "status", statusDecision)

				// Delete message after successful processing
				_, err = c.client.DeleteMessage(msgCtx, &sqs.DeleteMessageInput{
					QueueUrl:      aws.String(c.queueURL),
					ReceiptHandle: msg.ReceiptHandle,
				})
				if err != nil {
					status = "error"
					slog.Error("Error deleting message from SQS", "error", err)
					span.RecordError(err)
				}
				span.End()

				metricshelper.SQSConsumeTotal.WithLabelValues(c.queueURL, status).Inc()
				metricshelper.SQSConsumeDuration.WithLabelValues(c.queueURL, status).Observe(time.Since(start).Seconds())
			}
		}
	}
}

func (c *SQSConsumer) Close() error {
	return nil
}
