package consumer

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/seanchuatech/order-processing-system/services/analytics/internal/domain"
	"github.com/seanchuatech/order-processing-system/services/analytics/internal/metricshelper"
	"github.com/seanchuatech/order-processing-system/services/analytics/internal/otelhelper"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
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
	slog.Info("Analytics service SQS consumer starting...")
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

				tr := otel.Tracer("analytics-service")
				msgCtx, span := tr.Start(parentCtx, "ProcessAnalytics", trace.WithSpanKind(trace.SpanKindConsumer))

				event, err := parseMessageBody(*msg.Body)
				if err != nil {
					status = "error"
					slog.Error("Error parsing message body", "error", err)
					span.RecordError(err)
					span.End()
					metricshelper.SQSConsumeTotal.WithLabelValues(c.queueURL, status).Inc()
					metricshelper.SQSConsumeDuration.WithLabelValues(c.queueURL, status).Observe(time.Since(start).Seconds())
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

				slog.Info("Processed payment event",
					"order_id", event.OrderID,
					"status", event.Status,
					"amount", event.Amount,
					"total_revenue", totalRev,
					"successful_payments", successCount,
					"failed_payments", failedCount,
					"success_rate_percent", successRate,
				)

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
