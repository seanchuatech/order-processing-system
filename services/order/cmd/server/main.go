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
	defer func() { _ = db.Close() }()

	// 3. Run Database migrations/schema setup
	if err := setupDatabaseSchema(db); err != nil {
		slog.Error("Failed to set up database schema", "error", err)
		os.Exit(1)
	}

	// 4. Initialize Components
	orderRepo := repository.NewPostgresOrderRepository(db)
	orderService := service.NewOrderService(orderRepo)
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
			total_price_cents BIGINT NOT NULL,
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
			price_cents BIGINT NOT NULL
		);
	`
	outboxTable := `
		CREATE TABLE IF NOT EXISTS outbox (
			id VARCHAR(50) PRIMARY KEY,
			aggregate_id VARCHAR(50) NOT NULL,
			payload JSONB NOT NULL,
			status VARCHAR(20) NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL
		);
	`
	if _, err := db.Exec(ordersTable); err != nil {
		return fmt.Errorf("failed to create orders table: %w", err)
	}
	if _, err := db.Exec(orderItemsTable); err != nil {
		return fmt.Errorf("failed to create order_items table: %w", err)
	}
	if _, err := db.Exec(outboxTable); err != nil {
		return fmt.Errorf("failed to create outbox table: %w", err)
	}
	return nil
}
