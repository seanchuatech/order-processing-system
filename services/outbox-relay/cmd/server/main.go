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
)

type OutboxRecord struct {
	ID          string
	AggregateID string
	Payload     string
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	slog.Info("Starting Outbox Relay Service...")

	// 1. Load Configurations
	port := os.Getenv("PORT")
	if port == "" {
		port = "8095"
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/order_db?sslmode=disable"
	}
	sqsQueueURL := os.Getenv("SQS_QUEUE_URL")
	if sqsQueueURL == "" {
		sqsQueueURL = "http://localhost:4566/000000000000/order-pending"
	}
	sqsEndpoint := os.Getenv("SQS_ENDPOINT")

	// 2. Connect to Database (with retry loop)
	var db *sql.DB
	var err error
	for i := 1; i <= 10; i++ {
		db, err = sql.Open("postgres", dbURL)
		if err == nil {
			err = db.Ping()
		}
		if err == nil {
			slog.Info("Outbox Relay successfully connected to database")
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

	// 3. Initialize AWS Config and SQS Client
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("unable to load AWS config", "error", err)
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

	// 4. Start Health Check HTTP Server
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
		slog.Info("Health server listening", "port", port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Health server error", "error", err)
		}
	}()

	// 5. Start Polling Loop
	relayCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-relayCtx.Done():
				return
			case <-ticker.C:
				if err := processOutbox(relayCtx, db, sqsClient, sqsQueueURL); err != nil {
					slog.Error("Error processing outbox", "error", err)
				}
			}
		}
	}()

	// 6. Graceful Shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	slog.Info("Shutting down Outbox Relay Service gracefully...")
	cancel() // Stop polling loop

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server shutdown error", "error", err)
	}

	slog.Info("Outbox Relay Service stopped.")
}

func processOutbox(ctx context.Context, db *sql.DB, sqsClient *sqs.Client, queueURL string) error {
	// Start a transaction
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// Select pending records FOR UPDATE SKIP LOCKED to prevent multiple instances from grabbing same records
	query := `
		SELECT id, aggregate_id, payload
		FROM outbox
		WHERE status = 'pending'
		ORDER BY created_at ASC
		LIMIT 10
		FOR UPDATE SKIP LOCKED
	`
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query outbox: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []OutboxRecord
	for rows.Next() {
		var r OutboxRecord
		if err := rows.Scan(&r.ID, &r.AggregateID, &r.Payload); err != nil {
			return fmt.Errorf("failed to scan outbox record: %w", err)
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error during outbox rows iteration: %w", err)
	}
	_ = rows.Close() // Close early to avoid holding connection during SQS network call

	if len(records) == 0 {
		return nil
	}

	slog.Info("Processing outbox events", "count", len(records))

	for _, record := range records {
		// Send message to SQS
		_, err := sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
			QueueUrl:    aws.String(queueURL),
			MessageBody: aws.String(record.Payload),
		})
		if err != nil {
			return fmt.Errorf("failed to publish to SQS for record %s: %w", record.ID, err)
		}

		// Update record status to processed
		updateQuery := `
			UPDATE outbox
			SET status = 'processed'
			WHERE id = $1
		`
		_, err = tx.ExecContext(ctx, updateQuery, record.ID)
		if err != nil {
			return fmt.Errorf("failed to update status for record %s: %w", record.ID, err)
		}
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit outbox transaction: %w", err)
	}

	return nil
}
