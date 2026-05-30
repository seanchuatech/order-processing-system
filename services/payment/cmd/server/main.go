package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/seanchuatech/order-processing-system/services/payment/internal/consumer"
	"github.com/seanchuatech/order-processing-system/services/payment/internal/repository"
)

func main() {
	log.Println("Starting Payment Service...")

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
		log.Fatalf("unable to load AWS SDK config: %v", err)
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
		log.Println("Initializing Payment Service in LOCAL DIRECT SQS mode...")
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
		log.Println("Initializing Payment Service in SNS FANOUT mode...")
		if snsTopicARN == "" {
			log.Fatal("SNS_TOPIC_ARN must be set when LOCAL_MODE is false")
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
			log.Printf("Error closing SQS consumer: %v", err)
		}
	}()

	// 5. Health Check Endpoint
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy"}`))
	})

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	go func() {
		log.Printf("Health server listening on port %s", port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("Health server error: %v", err)
		}
	}()

	// 6. Start Consumer Loop in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := sqsConsumer.Start(ctx); err != nil {
			log.Printf("SQS consumer stopped: %v", err)
		}
	}()

	// 7. Graceful Shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down Payment Service gracefully...")
	cancel() // Stop consumer

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Payment Service stopped.")
}
