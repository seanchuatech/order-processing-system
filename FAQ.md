# Order Processing System — Frequently Asked Questions (FAQ)

This document addresses common questions regarding system design, scalability under load, reliability guarantees, and technology choices.

---

### 1. Scaling & High Traffic

#### Q1: How does this architecture handle sudden spikes in traffic (e.g., Black Friday)?
* **Asynchronous Decoupling**: The API endpoint (`POST /orders`) does not process payments or deduct stock synchronously. It performs a rapid write to the database and returns a `201 Created` code in milliseconds. The heavy lifting is offloaded to SQS queues.
* **SQS Queue Buffering**: SQS acts as a buffer. Even if downstream services are running slow, messages pile up safely in the queues instead of crashing the HTTP servers or exhausting database connection pools.
* **Automatic Scaling (HPA & Karpenter)**:
  * **Horizontal Pod Autoscalers (HPAs)** automatically spin up more replicas of our service pods if CPU or memory usage spikes.
  * **Karpenter** monitors the cluster for pods that cannot be scheduled due to resource constraints, dynamically rents new EC2 servers from AWS in seconds, and spins them down when traffic subsides.
* **Concurrency in Relays**: The `outbox-relay` uses a `FOR UPDATE SKIP LOCKED` query, enabling us to run multiple relay daemons simultaneously without them competing for or duplicating the same outbox rows.

---

### 2. Reliability & Resiliency

#### Q2: What happens if a downstream service (like the Payment Service) goes offline?
* **No Message Loss**: SQS queues retain messages (by default up to 4 days). If the Payment Service crashes, orders are still registered and placed in the queue. When the Payment Service is redeployed, it simply picks up where it left off, and the system catches up.
* **Eventual Consistency**: While the customer might wait a few seconds longer for their email confirmation or stock update, no data is lost, and the system state will eventually synchronize.

#### Q3: What happens if a message fails repeatedly (e.g., bad payload)?
* **Dead Letter Queues (DLQs)**: After a message fails processing 3 times (configured via the queue's redrive policy), SQS automatically moves it to a DLQ (e.g., `ops-sandbox-orders-created-dlq`).
* **Alerting**: A CloudWatch alarm monitors DLQ depth. If the DLQ contains messages, an alarm is triggered to notify developers to inspect and debug the faulty payload manually, preventing the main queue from being blocked.

#### Q4: How do we handle duplicate messages (Idempotency)?
* **Network Retries**: In distributed systems, messages can occasionally be delivered more than once (at-least-once delivery). 
* **Handling**: Downstream consumers must be designed to be **idempotent**. For instance:
  * The `inventory-service` tracks processed order transactions.
  * The database enforces unique constraints (e.g., an order can only have its stock deducted once).
  * If a service receives a message it has already processed, it simply acknowledges the message to SQS and discards the duplicate.

---

### 3. Design Decisions & Project Structure

#### Q5: Why did we build our HTTP routes using Go's standard library instead of frameworks like Gin or Echo?
* **Dependency Minimization**: Standard library `net/http` (specifically `http.ServeMux`) is highly optimized, stable, and secure. Eliminating third-party frameworks keeps our container images extremely small (~15MB static distroless) and drastically reduces the surface area for CVE security vulnerabilities.

#### Q6: Why do we use a Go workspace (`go.work`) instead of a single top-level module?
* **Independent Modules**: `go.work` enables a multi-module monorepo structure. Each microservice manages its own `go.mod` file and dependencies independently. This prevents dependency version conflicts (e.g., if one service needs a specific library version that another does not) and prevents monolithic build configurations, while still letting IDEs auto-import packages and navigate seamlessly across modules.

#### Q7: Why do we use distroless base Docker images?
* **Minimalist Container Footprint**: Distroless images (`gcr.io/distroless/static-debian12`) contain *only* the compiled binary and basic SSL/CA certificates. They do not contain package managers (like `apk` or `apt`), shells (like `sh` or `bash`), or standard Unix command line tools. This reduces the container image size to ~15MB and eliminates common operating system package vulnerabilities.

#### Q8: What is the purpose of running HTTP health servers on separate ports (like :8081 for notification-service)?
* **Backpressure Protection**: If a service's main thread is blocked (e.g., waiting on long-running SQS long-polls or congested database queries), we want Kubernetes liveness and readiness probes to check container health independently. Running health checks on a separate dedicated port ensures the Kubernetes control plane receives instant status replies without getting queued behind application workloads.

---

### 4. Security, Secrets & Telemetry

#### Q9: How do we secure database passwords and credentials in production?
* **Zero Hardcoded Secrets**: Secrets are never stored in Git or container images.
* **External Secrets Operator (ESO)**: In production, database credentials and connection strings are stored securely in **AWS SSM Parameter Store** (encrypted via a custom KMS key). ESO retrieves these credentials dynamically using EKS IAM Roles for Service Accounts (IRSA) and loads them directly into the pod's memory at runtime.

#### Q10: How does distributed tracing work across SQS and SNS boundaries?
* **Context Propagation**: SQS queues break standard HTTP headers. To maintain tracing context, we extract the OpenTelemetry `traceparent` metadata from the active Go context, serialize it, and insert it as SQS Message Attributes during event publication. The consuming microservice extracts these attributes, deserializes the trace ID, and starts a child span linked directly to the parent request.

---

### 5. Troubleshooting & Operations

#### Q11: How do you debug a running distroless container if it has no shell (`sh` or `bash`)?
* **Ephemeral Debug Containers**: We use Kubernetes ephemeral debugging:
  ```bash
  kubectl debug -it <pod-name> --image=busybox --target=<container-name>
  ```
  This attaches a temporary container containing standard debugging utilities directly to the target pod's network namespace and process tree.

#### Q12: Why does destroying the platform layer fail if the bootstrap layer was deleted first?
* **State Orphanage**: The Platform Terraform state file relies on EKS cluster APIs to uninstall Helm charts (like Ingress-Nginx or ArgoCD). If you destroy the Bootstrap layer first (which deletes EKS), the Platform layer can no longer connect to the API, causing the destroy command to hang. Always run `terraform destroy` in `terraform/platform` before running it in `terraform/bootstrap`.
