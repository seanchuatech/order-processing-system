package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/seanchuatech/order-processing-system/services/inventory/internal/consumer"
	"github.com/seanchuatech/order-processing-system/services/inventory/internal/otelhelper"
	"github.com/seanchuatech/order-processing-system/services/inventory/internal/repository"
)

func main() {
	// Configure structured JSON logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	slog.Info("Starting Inventory Service...")

	// Initialize OpenTelemetry Tracer
	ctx := context.Background()
	tp, err := otelhelper.InitTracer(ctx, "inventory-service")
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
		port = "8093"
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/order_db?sslmode=disable"
	}
	sqsQueueURL := os.Getenv("SQS_QUEUE_URL")
	if sqsQueueURL == "" {
		sqsQueueURL = "http://localhost:9324/000000000000/payment-processed-inventory"
	}
	sqsEndpoint := os.Getenv("SQS_ENDPOINT")

	// 2. Connect to Database (with retry loop)
	var db *sql.DB
	for i := 1; i <= 10; i++ {
		db, err = sql.Open("postgres", dbURL)
		if err == nil {
			err = db.Ping()
		}
		if err == nil {
			break
		}
		slog.Warn("Failed to connect to database, retrying...", "attempt", i, "max_attempts", 10, "error", err)
		time.Sleep(3 * time.Second)
	}
	if err != nil {
		slog.Error("unable to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	slog.Info("Connected to database successfully.")

	// 3. Database Schema Initialization & Seeding
	if err := initSchemaAndSeed(db); err != nil {
		slog.Error("failed to initialize schema and seed data", "error", err)
		os.Exit(1)
	}

	// 4. Initialize AWS SQS Client
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

	// 5. Initialize Components
	repo := repository.NewPostgresInventoryRepository(db)
	sqsConsumer := consumer.NewSQSConsumer(sqsClient, sqsQueueURL, repo)
	defer func() {
		if err := sqsConsumer.Close(); err != nil {
			slog.Error("Error closing SQS consumer", "error", err)
		}
	}()

	// 6. Set Up Health Check & Metrics Server
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

	// 7. Start SQS Consumer Loop
	consumerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := sqsConsumer.Start(consumerCtx); err != nil {
			slog.Error("SQS consumer stopped", "error", err)
		}
	}()

	// 8. Graceful Shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	slog.Info("Shutting down Inventory Service gracefully...")
	cancel() // Stop consumer

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server shutdown error", "error", err)
	}

	slog.Info("Inventory Service stopped.")
}

func initSchemaAndSeed(db *sql.DB) error {
	// Create table if not exists
	query := `
	CREATE TABLE IF NOT EXISTS inventory (
		product_id VARCHAR(255) PRIMARY KEY,
		quantity INT NOT NULL,
		updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create inventory table: %w", err)
	}
	slog.Info("Database schema check: 'inventory' table is ready.")

	// Seed some default inventory products if table is empty
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM inventory").Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to count inventory items: %w", err)
	}

	if count == 0 {
		slog.Info("Inventory table is empty. Seeding initial products...")
		seedProducts := map[string]int{
			"item-abc": 100,
			"item-xyz": 100,
		}
		for pid, qty := range seedProducts {
			_, err = db.Exec("INSERT INTO inventory (product_id, quantity) VALUES ($1, $2)", pid, qty)
			if err != nil {
				return fmt.Errorf("failed to seed product %s: %w", pid, err)
			}
		}
		slog.Info("Seeded default inventory successfully.")
	}

	return nil
}
