package otelhelper

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

// InitTracer initializes an OTLP tracer provider.
func InitTracer(ctx context.Context, serviceName string) (*sdktrace.TracerProvider, error) {
	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	return tp, nil
}

// SQSCarrier implements propagation.TextMapCarrier for SQS Message Attributes.
type SQSCarrier map[string]sqstypes.MessageAttributeValue

func (c SQSCarrier) Get(key string) string {
	if val, ok := c[key]; ok && val.StringValue != nil {
		return *val.StringValue
	}
	return ""
}

func (c SQSCarrier) Set(key string, value string) {
	c[key] = sqstypes.MessageAttributeValue{
		DataType:    aws.String("String"),
		StringValue: aws.String(value),
	}
}

func (c SQSCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// SNSCarrier implements propagation.TextMapCarrier for SNS Message Attributes.
type SNSCarrier map[string]snstypes.MessageAttributeValue

func (c SNSCarrier) Get(key string) string {
	if val, ok := c[key]; ok && val.StringValue != nil {
		return *val.StringValue
	}
	return ""
}

func (c SNSCarrier) Set(key string, value string) {
	c[key] = snstypes.MessageAttributeValue{
		DataType:    aws.String("String"),
		StringValue: aws.String(value),
	}
}

func (c SNSCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}
