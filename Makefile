.PHONY: lint test helm-lint docker-up docker-down build test-cover

# Directory definitions (append new services here when scaffolded, e.g. services/payment)
SERVICES = services/order services/notification services/payment services/inventory services/analytics

lint:
	@echo "==> Running golangci-lint on all services..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		for dir in $(SERVICES); do \
			echo "Linting $$dir..."; \
			(cd $$dir && golangci-lint run ./...); \
		done; \
	else \
		echo "golangci-lint not installed. Skipping. Install via: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

test:
	@echo "==> Running unit tests on all services..."
	@for dir in $(SERVICES); do \
		echo "Testing $$dir..."; \
		(cd $$dir && go test -v ./...); \
	done

test-cover:
	@echo "==> Generating test coverage profiles..."
	@for dir in $(SERVICES); do \
		echo "Coverage for $$dir..."; \
		(cd $$dir && go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out -o coverage.html); \
	done

helm-lint:
	@echo "==> Linting app-generic Helm chart..."
	@if command -v helm >/dev/null 2>&1; then \
		helm lint helm-charts/app-generic; \
	else \
		echo "helm not installed. Skipping."; \
	fi

docker-up:
	@echo "==> Starting local infrastructure (PostgreSQL, ElasticMQ, Jaeger, Prometheus, Grafana) and Go services..."
	docker compose up -d --build

docker-down:
	@echo "==> Stopping local infrastructure..."
	docker compose down -v

build:
	@echo "==> Building Go binary executables..."
	@for dir in $(SERVICES); do \
		echo "Building $$dir..."; \
		(cd $$dir && go build -o bin/server ./cmd/server); \
	done
