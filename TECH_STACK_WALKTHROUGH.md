# Order Processing System — Tech Stack Walkthrough

This document provides an in-depth breakdown of the technology stack, software architectures, packages, and frameworks utilized across every layer of the **Order Processing System**.

---

## 1. Application & Go Services Layer

Our 6 microservices (`order`, `outbox-relay`, `payment`, `inventory`, `notification`, and `analytics`) are written in Go and structured to prioritize speed, simplicity, and low memory consumption.

### Frameworks & Libraries Used
* **Routing & HTTP Server**: Native Go standard library `net/http` (specifically `http.ServeMux`).
  * *Why*: Rather than importing heavy web frameworks (like Gin, Echo, or Fiber), we use standard library routing (enhanced with method matching in modern Go versions). This keeps binaries lightweight, minimizes third-party dependency vulnerabilities, and speeds up compile times.
* **Structured Logging**: Native Go standard library `log/slog`.
  * *Why*: `log/slog` outputs JSON logs directly to `stdout`. Centralized logging platforms (such as Fluent Bit or AWS CloudWatch) consume JSON streams out-of-band, avoiding CPU-heavy logging overhead inside the microservices.
* **Database Driver**: Native PostgreSQL driver `github.com/lib/pq` wrapping standard `database/sql`.
  * *Why*: We do not use heavy Object-Relational Mappers (ORMs) like GORM. Instead, we write optimized raw SQL. This ensures transaction boundaries are explicitly controlled and database connections operate with minimal abstraction overhead.
* **UUID Generation**: `github.com/google/uuid` for generating unique order and event identifiers.

### Architecture Pattern: Clean Architecture (Hexagonal / Ports & Adapters)
Each service decouples core logic from infrastructure details by structuring its codebase into:
* **`domain/` (Entities & Interfaces)**: Pure, framework-free business models (e.g., `Order`, `OrderItem`) and interface contracts (e.g., `OrderRepository`).
* **`service/` (Core Logic)**: Implements domain-specific use cases (e.g., calculating totals, initiating database transactions).
* **`repository/` (Infrastructure Adapters)**: Implements database queries, SQS publishing, and external system integrations.
* **`handler/` (Entrypoint Adapters)**: Binds incoming HTTP requests or SQS polling loops to service controllers.

---

## 2. Event-Driven Messaging Layer

Decoupled asynchronous coordination is managed via **AWS SQS** (Simple Queue Service) and **AWS SNS** (Simple Notification Service).

### Frameworks & Libraries Used
* **AWS SDK for Go v2**:
  * `github.com/aws/aws-sdk-go-v2` & `github.com/aws/aws-sdk-go-v2/config`: Baseline AWS configuration loader.
  * `github.com/aws/aws-sdk-go-v2/service/sqs` & `github.com/aws/aws-sdk-go-v2/service/sns`: Client wrappers for message queuing and fanout pub/sub.
  * *Why*: We use AWS SDK v2 because it is thread-safe, modular (allowing services to import only the service APIs they need, like SQS, without importing the whole SDK), and uses Go context parameters for clean timeout propagation.

### Architecture Pattern: Transactional Outbox Pattern
To prevent dual-write bugs (e.g., database commit succeeds but SQS publish fails, or vice versa), we write events atomically:
1. **Atomic Commit**: The `order-service` writes the order record and the event payload into an `outbox` table inside PostgreSQL within a single ACID database transaction.
2. **Asynchronous Relay**: The `outbox-relay` background daemon polls the `outbox` table using a concurrency-safe query:
   ```sql
   SELECT id, aggregate_id, payload
   FROM outbox
   WHERE status = 'pending'
   ORDER BY created_at ASC
   LIMIT 10
   FOR UPDATE SKIP LOCKED;
   ```
   * *Why `FOR UPDATE SKIP LOCKED`?*: This locks matching rows and skips already-locked rows, allowing us to safely scale the `outbox-relay` to multiple pods without duplicate message deliveries.
3. **Queue Publishing**: The relay publishes the event to SQS and marks the row status as `processed`.

---

## 3. Infrastructure as Code (IaC) Layer

We split infrastructure management into two decoupled layers using **Terraform** to enforce security and resource isolation.

### Core Modules & Add-Ons Used
* **Layer 1: Bootstrap (`terraform/bootstrap`)**:
  * Provisions AWS VPC configurations (isolated subnets, NAT gateways, route tables).
  * Boots the Amazon EKS cluster control plane and sets up the EKS OpenID Connect (OIDC) Identity Provider.
* **Layer 2: Platform (`terraform/platform`)**:
  * **EKS Autoscaling (Karpenter)**: Automatically scales worker nodes. Rather than using legacy Kubernetes Cluster Autoscalers (which scale nodes using pre-defined AWS Auto Scaling Groups), Karpenter evaluates pod resources requests directly, provisions right-sized EC2 compute instances on-the-fly, and consolidates idle nodes to minimize costs.
  * **Relational Database (RDS)**: Deploys PostgreSQL 16 in private database subnets with forced SSL encryption (`sslmode=require`).
  * **Secrets Management (External Secrets Operator - ESO)**: Avoids hardcoding database connection strings and SQS URLs. SSM Parameter Store parameters are encrypted via a KMS Customer Managed Key (CMK) and dynamically mapped to native Kubernetes secrets using EKS ServiceAccount OIDC federated roles (IAM Roles for Service Accounts - IRSA).

---

## 4. GitOps & Kubernetes Control Plane

The Kubernetes cluster runs on a strict **GitOps Continuous Deployment** model managed via **ArgoCD**.

### Core Configurations Used
* **"Golden Path" Reusable Helm Chart (`app-generic`)**:
  * Instead of maintaining separate deployment, service, ingress, and auto-scaler YAML configuration files for each microservice, we developed a unified template.
  * Standardizes configurations like checksum annotations (`checksum/config`), liveness/readiness health probes, resource limits, and Horizontal Pod Autoscaler (HPA) rules.
* **ArgoCD GitOps Engine**:
  * Reconciles EKS cluster configurations against the Git state located in `argocd/apps/base/` (applications configuration) and `/local` or `/production` (environment specific configurations).
  * Automatically detects and overrides manual configuration edits (drift prevention) to keep EKS in sync with the repository.

---

## 5. Observability & Distributed Telemetry Layer

Because requests span multiple microservices across asynchronous queuing boundaries, distributed context propagation is critical.

### Frameworks & Libraries Used
* **OpenTelemetry SDK for Go**:
  * `go.opentelemetry.io/otel`: Core API registry.
  * `go.opentelemetry.io/otel/sdk`: Configuration and exporter mapping.
  * `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`: Automatically tracks HTTP requests and server spans.
* **Distributed Tracing (Jaeger)**:
  * Traces are collected via OTLP/gRPC connections pointing to a Jaeger collector.
  * **SQS Context Propagation Middleware**: SQS queues break direct HTTP context headers. To maintain single-trace lineage, we serialize OpenTelemetry's `traceparent` headers into SQS message attributes during publication, and deserialize them on consumption.
* **Prometheus Metrics**:
  * Services expose a standard `/metrics` HTTP endpoint using the Prometheus Go client `github.com/prometheus/client_golang`.
  * Grafana dashboards connect to the Prometheus metrics scraper to visualize system health, request volumes, queue latency, and database connection pools.

---

## 6. Local Sandbox Environment

To support local testing without incurring AWS cloud charges, we simulate the cloud stack in Docker:

* **LocalStack**: Emulates AWS SQS, SNS, and KMS services locally.
* **ElasticMQ**: Fast, lightweight SQS-compatible mock server used to speed up local testing loops.
* **Kind (Kubernetes in Docker)**: Runs a local Kubernetes cluster.
* **Bridge Gateway Router (`external-services.yaml`)**: Maps Kind cluster namespaces to local host ports, allowing containerized pods in Kind to connect directly to LocalStack and Postgres running on the host Docker bridge network.

---

## 7. CI/CD Quality Control Gates

Our automation workflows are orchestrated via **GitHub Actions** and divided into three dedicated pipelines:

1. **Go Quality Gate (`ci.yaml`)**:
   * Runs `golangci-lint` to check code quality and formatting.
   * Runs unit and integration tests with concurrency race detection (`go test -race`).
   * Validates Terraform configurations (`terraform validate`).
2. **Security Scan Gate (`security.yaml`)**:
   * Scans Go code using `gosec` for vulnerability patterns.
   * Scans container images using `trivy`.
   * Audits Infrastructure as Code configurations using `checkov`.
3. **Build & Release Gate (`build-push.yaml`)**:
   * Generates multi-architecture container images (`linux/amd64`, `linux/arm64`) using Docker Buildx.
   * Publishes images to the GitHub Container Registry (GHCR).
   * Automatically updates Helm values in Git with the new image SHA tags and raises a Pull Request back to the `main` branch to trigger GitOps reconciliation.
