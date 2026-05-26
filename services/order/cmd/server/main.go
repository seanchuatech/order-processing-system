package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/seanchuatech/order-processing-system/services/order/internal/handler"
	"github.com/seanchuatech/order-processing-system/services/order/internal/repository"
	"github.com/seanchuatech/order-processing-system/services/order/internal/service"
)

func main() {
	log.Println("Starting Order Service...")

	// 1. Load Configurations
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/order_db?sslmode=disable"
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

	// 2. Connect to Database (with retry loop for docker-compose start sequence)
	var db *sql.DB
	var err error
	for i := 1; i <= 10; i++ {
		db, err = sql.Open("postgres", dbURL)
		if err == nil {
			err = db.Ping()
		}
		if err == nil {
			log.Println("Successfully connected to the database")
			break
		}
		log.Printf("Failed to connect to database (attempt %d/10): %v. Retrying in 3s...", i, err)
		time.Sleep(3 * time.Second)
	}
	if err != nil {
		log.Fatalf("Could not connect to database after retries: %v", err)
	}
	defer db.Close()

	// 3. Run Database migrations/schema setup
	if err := setupDatabaseSchema(db); err != nil {
		log.Fatalf("Failed to set up database schema: %v", err)
	}

	// 4. Initialize Components
	orderRepo := repository.NewPostgresOrderRepository(db)
	eventPublisher := repository.NewKafkaEventPublisher(kafkaBrokers, kafkaTopic)
	defer func() {
		if err := eventPublisher.Close(); err != nil {
			log.Printf("Error closing Kafka event publisher: %v", err)
		}
	}()

	orderService := service.NewOrderService(orderRepo, eventPublisher)
	orderHandler := handler.NewOrderHandler(orderService)

	mux := http.NewServeMux()
	orderHandler.RegisterRoutes(mux)

	// 5. Start Server
	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	go func() {
		log.Printf("Order service server listening on port %s", port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	// 6. Graceful Shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}
	log.Println("Order Service stopped.")
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
