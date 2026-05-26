package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/seanchuatech/order-processing-system/services/notification/internal/consumer"
)

func main() {
	log.Println("Starting Notification Service...")

	// 1. Load Configurations
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081" // Different port to avoid conflict with order-service
	}
	kafkaBrokersStr := os.Getenv("KAFKA_BROKERS")
	if kafkaBrokersStr == "" {
		kafkaBrokersStr = "localhost:9092"
	}
	kafkaBrokers := strings.Split(kafkaBrokersStr, ",")
	kafkaTopic := os.Getenv("KAFKA_TOPIC")
	if kafkaTopic == "" {
		kafkaTopic = "orders.created"
	}
	kafkaGroupID := os.Getenv("KAFKA_GROUP_ID")
	if kafkaGroupID == "" {
		kafkaGroupID = "notification-group"
	}

	// 2. Initialize Components
	kafkaConsumer := consumer.NewKafkaConsumer(kafkaBrokers, kafkaTopic, kafkaGroupID)
	defer func() {
		if err := kafkaConsumer.Close(); err != nil {
			log.Printf("Error closing Kafka consumer: %v", err)
		}
	}()

	// 3. Set Up Health Check Server
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy"}`))
	})

	healthServer := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	// Run Health check HTTP server in background
	go func() {
		log.Printf("Health server listening on port %s", port)
		if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("Health server error: %v", err)
		}
	}()

	// 4. Start Kafka Consumer Loop in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := kafkaConsumer.Start(ctx); err != nil {
			log.Printf("Kafka consumer stopped with error: %v", err)
		}
	}()

	// 5. Graceful Shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down gracefully...")
	cancel() // Stop the Kafka consumer loop

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := healthServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("Health server shutdown error: %v", err)
	}

	log.Println("Notification Service stopped.")
}
