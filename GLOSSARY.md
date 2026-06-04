# Order Processing System — Glossary of Technical Terms

This glossary provides brief, simplified explanations of key DevOps, backend, and architectural concepts used in this repository.

---

### Core Database & Pattern Concepts

* **ACID (Atomicity, Consistency, Isolation, Durability)**
  * *What it is*: A set of database guarantees that ensure transactions are processed reliably.
  * *In plain English*: 
    * **Atomicity**: "All or nothing." Either the entire transaction succeeds, or it all fails and rolls back.
    * **Consistency**: Validates database rules (e.g. data types, constraints).
    * **Isolation**: Transactions running at the same time don't interfere with each other.
    * **Durability**: Once a transaction is saved, it stays saved, even if power goes out.

* **Transactional Outbox Pattern**
  * *What it is*: A design pattern used to reliably publish events in microservices.
  * *In plain English*: Instead of performing a database commit and then immediately trying to send a message to a queue (which could fail if the queue is down), you write both the database records and the event details into an "Outbox" table *in the same ACID database transaction*. A separate background worker (the Outbox Relay) then reads from this table and sends the events to the queue safely.

* **`FOR UPDATE SKIP LOCKED`**
  * *What it is*: A database locking mechanism used for creating high-performance queues.
  * *In plain English*: When reading rows from a database to process them, this tells the database to lock the selected rows so no other worker grabs them, and to immediately *skip* over any rows that are already locked by other workers. This allows you to run multiple worker instances in parallel without duplicate processing.

---

### Event-Driven Messaging Concepts

* **Event-Driven Choreography**
  * *What it is*: A method of microservice communication where services react to events rather than being told what to do by a central orchestrator.
  * *In plain English*: Services act like dancers who know their steps. When Service A finishes a job, it broadcasts an event. Services B, C, and D are listening and react to that event on their own terms.

* **SQS (Simple Queue Service)**
  * *What it is*: A managed point-to-point message queuing service provided by AWS.
  * *In plain English*: A digital mailbox where messages wait in line to be read and processed by a single consumer service.

* **SNS (Simple Notification Service)**
  * *What it is*: A managed pub/sub (publisher/subscriber) messaging service provided by AWS.
  * *In plain English*: A digital megaphone. A service publishes a message once, and SNS instantly broadcasts (fans out) that message to multiple subscribers (like different SQS queues) at the same time.

* **Fanout Pattern**
  * *What it is*: Using an SNS topic to duplicate a single message into multiple SQS queues.
  * *In plain English*: One event (like "payment-processed") is broadcast via SNS and automatically replicated into three separate queues for Notification, Inventory, and Analytics so they can all process the event in parallel without getting in each other's way.

* **Dead Letter Queue (DLQ)**
  * *What it is*: A secondary queue where messages are sent if they fail to process.
  * *In plain English*: If a message is corrupt and keeps crashing a consumer service, after 3 failed attempts it gets moved to a "Dead Letter Queue" so it doesn't block other healthy messages in line.

---

### Cloud & Kubernetes (GitOps) Concepts

* **GitOps**
  * *What it is*: A practice where Git is the single source of truth for infrastructure and deployments.
  * *In plain English*: Instead of running manual commands like `kubectl apply` to deploy apps, you commit YAML configurations to Git. A controller (like ArgoCD) watches Git and automatically applies the changes to the cluster.

* **ArgoCD**
  * *What it is*: A GitOps continuous delivery tool for Kubernetes.
  * *In plain English*: The agent that sits in your Kubernetes cluster, compares what is running on the cluster with what is in Git, and auto-syncs them (including fixing any manual edits, which is called *drift reconciliation*).

* **Karpenter**
  * *What it is*: A high-performance Kubernetes node autoscaler built by AWS.
  * *In plain English*: Instead of using static server groups, Karpenter monitors pods that have no room to run, goes directly to AWS, leases a new server of the exact size needed, and shuts it down when no longer used to save money.

* **IRSA (IAM Roles for Service Accounts)**
  * *What it is*: AWS IAM credential federation for Kubernetes pods.
  * *In plain English*: Allows Kubernetes pods to have fine-grained permissions (e.g. read only one SQS queue) using secure temporary AWS tokens instead of saving permanent password keys inside the container code.

* **External Secrets Operator (ESO)**
  * *What it is*: An operator that syncs external secrets managers into Kubernetes native Secrets.
  * *In plain English*: It securely pulls passwords/keys from AWS SSM Parameter Store and loads them into Kubernetes memory when the app starts, so developers never write passwords in Git configurations.

---

### Observability Concepts

* **OpenTelemetry (OTel)**
  * *What it is*: An open-source standard framework for collecting logs, metrics, and traces.
  * *In plain English*: A unified SDK that lets you instrument your code once to output telemetry data to any monitoring tool (like Jaeger, Prometheus, or Datadog).

* **Context Propagation**
  * *What it is*: The process of passing tracking IDs across network boundaries.
  * *In plain English*: When a client requests an order, it receives a Trace ID. Context Propagation is the practice of carrying that Trace ID along in SQS message attributes and HTTP headers so that downstream services can attach their logs to the same ID, letting you trace the entire journey of a request.
