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
	"github.com/seanchuatech/order-processing-system/services/order/internal/handler"
	"github.com/seanchuatech/order-processing-system/services/order/internal/metricshelper"
	"github.com/seanchuatech/order-processing-system/services/order/internal/otelhelper"
	"github.com/seanchuatech/order-processing-system/services/order/internal/repository"
	"github.com/seanchuatech/order-processing-system/services/order/internal/service"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func main() {
	// Configure structured JSON logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	slog.Info("Starting Order Service...")

	// Initialize OpenTelemetry Tracer
	ctx := context.Background()
	tp, err := otelhelper.InitTracer(ctx, "order-service")
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
		port = "8080"
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/order_db?sslmode=disable"
	}
	sqsQueueURL := os.Getenv("SQS_QUEUE_URL")
	if sqsQueueURL == "" {
		sqsQueueURL = "http://localhost:9324/000000000000/order-pending"
	}
	sqsEndpoint := os.Getenv("SQS_ENDPOINT")

	// 2. Connect to Database (with retry loop for docker-compose start sequence)
	var db *sql.DB
	for i := 1; i <= 10; i++ {
		db, err = sql.Open("postgres", dbURL)
		if err == nil {
			err = db.Ping()
		}
		if err == nil {
			slog.Info("Successfully connected to the database")
			break
		}
		slog.Warn("Failed to connect to database, retrying...", "attempt", i, "max_attempts", 10, "error", err)
		time.Sleep(3 * time.Second)
	}
	if err != nil {
		slog.Error("Could not connect to database after retries", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// 3. Run Database migrations/schema setup
	if err := setupDatabaseSchema(db); err != nil {
		slog.Error("Failed to set up database schema", "error", err)
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
	orderRepo := repository.NewPostgresOrderRepository(db)
	eventPublisher := repository.NewSQSEventPublisher(sqsClient, sqsQueueURL)
	defer func() {
		if err := eventPublisher.Close(); err != nil {
			slog.Error("Error closing SQS event publisher", "error", err)
		}
	}()

	orderService := service.NewOrderService(orderRepo, eventPublisher)
	orderHandler := handler.NewOrderHandler(orderService)

	mux := http.NewServeMux()
	orderHandler.RegisterRoutes(mux)

	// Register Prometheus metrics endpoint
	mux.Handle("GET /metrics", promhttp.Handler())

	// Wrap handler in OpenTelemetry HTTP instrumentation middleware and metrics middleware
	otelHandler := otelhttp.NewHandler(mux, "order-service")
	metricsHandler := metricshelper.MetricsMiddleware(otelHandler)

	// 5. Start Server
	server := &http.Server{
		Addr:    ":" + port,
		Handler: metricsHandler,
	}

	go func() {
		slog.Info("Order service server listening", "port", port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server failed", "error", err)
			os.Exit(1)
		}
	}()

	// 6. Graceful Shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	slog.Info("Shutting down gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("HTTP server shutdown error", "error", err)
	}
	slog.Info("Order Service stopped.")
}

func setupDatabaseSchema(db *sql.DB) error {
	ordersTable := `
		CREATE TABLE IF NOT EXISTS orders (
			id VARCHAR(50) PRIMARY KEY,
			customer_id VARCHAR(50) NOT NULL,
			total_price NUMERIC(10, 2) NOT NULL,
			status VARCHAR(20) NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL
		);
	`
	orderItemsTable := `
		CREATE TABLE IF NOT EXISTS order_items (
			id SERIAL PRIMARY KEY,
			order_id VARCHAR(50) NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
			product_id VARCHAR(50) NOT NULL,
			quantity INT NOT NULL,
			price NUMERIC(10, 2) NOT NULL
		);
	`
	if _, err := db.Exec(ordersTable); err != nil {
		return fmt.Errorf("failed to create orders table: %w", err)
	}
	if _, err := db.Exec(orderItemsTable); err != nil {
		return fmt.Errorf("failed to create order_items table: %w", err)
	}
	return nil
}
