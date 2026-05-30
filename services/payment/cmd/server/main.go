package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/seanchuatech/order-processing-system/services/payment/internal/consumer"
	"github.com/seanchuatech/order-processing-system/services/payment/internal/otelhelper"
	"github.com/seanchuatech/order-processing-system/services/payment/internal/repository"
)

func main() {
	// Configure structured JSON logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	slog.Info("Starting Payment Service...")

	// Initialize OpenTelemetry Tracer
	ctx := context.Background()
	tp, err := otelhelper.InitTracer(ctx, "payment-service")
	if err != nil {
		slog.Error("Failed to initialize tracer", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := tp.Shutdown(ctx); err != nil {
			slog.Error("Error shutting down tracer provider", "error", err)
		}
	}()

	// 1. Load Configurations
	port := os.Getenv("PORT")
	if port == "" {
		port = "8092"
	}
	sqsQueueURL := os.Getenv("SQS_QUEUE_URL")
	if sqsQueueURL == "" {
		sqsQueueURL = "http://localhost:9324/000000000000/order-pending"
	}
	sqsEndpoint := os.Getenv("SQS_ENDPOINT")
	snsTopicARN := os.Getenv("SNS_TOPIC_ARN")
	localMode := os.Getenv("LOCAL_MODE") == "true"

	// 2. Initialize AWS Config
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		slog.Error("unable to load AWS SDK config", "error", err)
		os.Exit(1)
	}

	var sqsClient *sqs.Client
	if sqsEndpoint != "" {
		sqsClient = sqs.NewFromConfig(cfg, func(o *sqs.Options) {
			o.BaseEndpoint = aws.String(sqsEndpoint)
		})
	} else {
		sqsClient = sqs.NewFromConfig(cfg)
	}

	// 3. Initialize Event Publisher based on Local/Prod Mode
	var publisher repository.EventPublisher
	if localMode {
		slog.Info("Initializing Payment Service in LOCAL DIRECT SQS mode...")
		notifURL := os.Getenv("SQS_NOTIFICATION_QUEUE_URL")
		if notifURL == "" {
			notifURL = "http://localhost:9324/000000000000/payment-processed-notification"
		}
		invURL := os.Getenv("SQS_INVENTORY_QUEUE_URL")
		if invURL == "" {
			invURL = "http://localhost:9324/000000000000/payment-processed-inventory"
		}
		analyticsURL := os.Getenv("SQS_ANALYTICS_QUEUE_URL")
		if analyticsURL == "" {
			analyticsURL = "http://localhost:9324/000000000000/payment-processed-analytics"
		}

		publisher = repository.NewSQSEventPublisher(sqsClient, []string{notifURL, invURL, analyticsURL})
	} else {
		slog.Info("Initializing Payment Service in SNS FANOUT mode...")
		if snsTopicARN == "" {
			slog.Error("SNS_TOPIC_ARN must be set when LOCAL_MODE is false")
			os.Exit(1)
		}
		var snsClient *sns.Client
		if sqsEndpoint != "" { // local emulation if needed, but normally not used
			snsClient = sns.NewFromConfig(cfg, func(o *sns.Options) {
				o.BaseEndpoint = aws.String(sqsEndpoint)
			})
		} else {
			snsClient = sns.NewFromConfig(cfg)
		}
		publisher = repository.NewSNSEventPublisher(snsClient, snsTopicARN)
	}

	// 4. Initialize SQS Consumer
	sqsConsumer := consumer.NewSQSConsumer(sqsClient, sqsQueueURL, publisher)
	defer func() {
		if err := sqsConsumer.Close(); err != nil {
			slog.Error("Error closing SQS consumer", "error", err)
		}
	}()

	// 5. Health Check & Metrics Endpoint
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy"}`))
	})
	mux.Handle("GET /metrics", promhttp.Handler())

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	go func() {
		slog.Info("Health server listening", "port", port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Health server error", "error", err)
		}
	}()

	// 6. Start Consumer Loop in background
	consumerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := sqsConsumer.Start(consumerCtx); err != nil {
			slog.Error("SQS consumer stopped", "error", err)
		}
	}()

	// 7. Graceful Shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	slog.Info("Shutting down Payment Service gracefully...")
	cancel() // Stop consumer

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server shutdown error", "error", err)
	}

	slog.Info("Payment Service stopped.")
}
