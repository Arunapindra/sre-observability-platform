# Architecture

This document provides a deep technical walkthrough of the SRE Observability Platform's architecture, from the microservices layer through the observability stack, infrastructure-as-code, and CI/CD pipelines.

---

## Table of Contents

1. [High-Level Architecture](#1-high-level-architecture)
2. [Microservices Layer](#2-microservices-layer)
3. [Observability Stack](#3-observability-stack)
4. [SRE Methodology Deep Dive](#4-sre-methodology-deep-dive)
5. [Terraform Modules](#5-terraform-modules)
6. [Kubernetes Deployment](#6-kubernetes-deployment)
7. [CI/CD Pipeline](#7-cicd-pipeline)
8. [Data Flow](#8-data-flow)

---

## 1. High-Level Architecture

```
                          +-------------------------------------------------+
                          |              Load Generator (:8090)              |
                          |  Diurnal traffic pattern (cosine function)       |
                          |  Burst simulation (5x multiplier, 2% chance)    |
                          |  Weighted endpoint selection                    |
                          +----+------------------+------------------+------+
                               |                  |                  |
                     GET/POST /api/orders   GET/POST /api/payments   GET/POST /api/users
                               |                  |                  |
                               v                  v                  v
+------------------------------+------------------+------------------+-------------------+
|                            Microservices Layer (Docker Network: backend)                |
|                                                                                         |
|  +------------------+     +---------------------+     +------------------+              |
|  | Order Service    |---->| Payment Service     |     | User Service     |              |
|  | :8081            |     | :8082               |     | :8083            |              |
|  |                  |---->|                     |     |                  |              |
|  | Circuit Breakers |     | Fraud Check (CB)    |     | In-memory Cache  |              |
|  | ~2% error rate   |     | ~5% error rate      |     | ~0.1% error rate |              |
|  | /metrics         |     | /metrics            |     | /metrics         |              |
|  +--------+---------+     +----------+----------+     +--------+---------+              |
|           |                          |                          |                        |
+-----------|--------------------------|--------------------------|------------------------+
            |   Prometheus scrape      |    every 15s             |
            v           (/metrics)     v                          v
+----------------------------------------------------------------------------------------------+
|                         Observability Stack (Docker Network: monitoring)                       |
|                                                                                               |
|  +-----------------+    +------------------+    +-------------------+    +-----------------+  |
|  | Prometheus      |    | Alertmanager     |    | Loki              |    | Promtail        |  |
|  | :9090           |--->| :9093            |    | :3100             |<---| (log shipper)   |  |
|  |                 |    |                  |    |                   |    |                 |  |
|  | Recording Rules |    | Route Tree:      |    | WAL + TSDB Schema |    | Docker socket   |  |
|  | SLO Rules       |    |  critical->PD    |    | 31-day retention  |    | scrape          |  |
|  | Alert Rules     |    |  warning->Slack  |    |                   |    |                 |  |
|  | 30-day TSDB     |    |  info->Email     |    |                   |    |                 |  |
|  +--------+--------+    +------------------+    +-------------------+    +-----------------+  |
|           |                                              |                                    |
|           +---------------------+------------------------+                                    |
|                                 |                                                             |
|                                 v                                                             |
|                    +---------------------------+                     +------------------+      |
|                    | Grafana :3000              |                     | Node Exporter    |      |
|                    |                           |                     | :9100            |      |
|                    | Datasources (provisioned): |                     | Host CPU/mem/    |      |
|                    |   Prometheus, Loki,        |                     | disk/network     |      |
|                    |   Alertmanager             |                     +------------------+      |
|                    |                           |                                              |
|                    | Dashboards (provisioned):  |                                              |
|                    |   SLO Overview             |                                              |
|                    |   Service Health           |                                              |
|                    |   Infrastructure           |                                              |
|                    +---------------------------+                                              |
+----------------------------------------------------------------------------------------------+
```

---

## 2. Microservices Layer

All four microservices are written in Go, use the `chi` router for HTTP handling, and instrument their endpoints with the Prometheus client library. They are built as multi-stage Docker images (build with `golang:1.22-alpine`, run on `alpine:3.20`) and run as non-root users inside containers.

### 2.1 Order Service (port 8081)

**Purpose:** Simulates an e-commerce order management API. Demonstrates inter-service communication and the circuit breaker pattern.

**Endpoints:**

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/orders` | List all orders |
| POST | `/api/orders` | Create a new order (calls user-service and payment-service) |
| GET | `/api/orders/{orderID}` | Get a specific order |
| GET | `/healthz` | Liveness probe |
| GET | `/readyz` | Readiness probe (returns 503 for 2 seconds on startup) |
| GET | `/metrics` | Prometheus metrics endpoint |

**Behavior:**
- Simulated error rate of ~2% on all business endpoints
- When creating an order, the service makes two downstream calls:
  1. `GET http://user-service:8083/api/users/validate` -- validates the user
  2. `POST http://payment-service:8082/api/payments` -- processes payment
- Both downstream calls are protected by **circuit breakers** (Sony gobreaker library)
- Circuit breaker configuration: trips when 50% of requests fail (minimum 5 requests), half-open after 30 seconds, allows 3 probe requests in half-open state
- Latency simulation: base 50ms for reads, 200ms for order creation, with normal-distribution jitter and occasional tail latency spikes (3-10x slower)

**Prometheus Metrics Exposed:**
- `http_requests_total{method, path, status}` -- request counter
- `http_request_duration_seconds{method, path}` -- latency histogram (11 buckets: 5ms to 10s)
- `orders_created_total` -- orders successfully created
- `orders_in_progress` -- current in-flight order creations (gauge)
- `order_processing_duration_seconds` -- end-to-end order processing time
- `downstream_requests_total{service, status}` -- calls to payment-service and user-service
- `circuit_breaker_state{service}` -- 0=closed, 1=half-open, 2=open

### 2.2 Payment Service (port 8082)

**Purpose:** Simulates a payment gateway with multiple payment types, each with different latency characteristics.

**Endpoints:**

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/payments` | List payments |
| POST | `/api/payments` | Process a new payment |
| GET | `/api/payments/{paymentID}` | Get a specific payment |
| GET | `/healthz` | Liveness probe |
| GET | `/readyz` | Readiness probe |
| GET | `/metrics` | Prometheus metrics endpoint |

**Behavior:**
- Higher simulated error rate of ~5% (intentional for interesting SLO data)
- Four payment types with different latency profiles:
  - **credit_card**: ~150ms base, 50ms jitter
  - **debit_card**: ~180ms base, 60ms jitter
  - **bank_transfer**: ~500ms base, 200ms jitter (slowest)
  - **digital_wallet**: ~100ms base, 30ms jitter (fastest)
- Internal fraud detection check via circuit breaker (~3% failure rate)
- Error types: "declined" (most common), "fraud_check_failed", "gateway_error"

**Prometheus Metrics Exposed:**
- `http_requests_total{method, path, status}` -- request counter
- `http_request_duration_seconds{method, path}` -- latency histogram
- `payment_transactions_total{status, type}` -- payment outcome by type
- `payment_amount_total{currency}` -- total payment amount processed
- `payment_processing_duration_seconds` -- processing time histogram
- `payments_in_flight` -- current in-flight payments (gauge)
- `downstream_requests_total{service, status}` -- fraud detection calls
- `circuit_breaker_state{service}` -- fraud detection circuit breaker

### 2.3 User Service (port 8083)

**Purpose:** Simulates a user management and authentication service with caching and session tracking.

**Endpoints:**

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/users` | List users |
| POST | `/api/users` | Create a new user |
| GET | `/api/users/validate` | Validate a user (called by order-service) |
| GET | `/api/users/{userID}` | Get a specific user (with cache) |
| POST | `/api/users/auth` | Authenticate a user |
| GET | `/healthz` | Liveness probe |
| GET | `/readyz` | Readiness probe |
| GET | `/metrics` | Prometheus metrics endpoint |

**Behavior:**
- Very low simulated error rate of ~0.1% (high-reliability service)
- In-memory cache pre-populated with 50 users (usr-100 through usr-149)
- Cache-aside pattern: check cache first, fall back to simulated DB query on miss, then populate cache
- Authentication simulation with realistic outcomes: 85% success, 7% invalid credentials, 5% account locked, 3% rate limited
- Active session count gauge with diurnal pattern (higher during business hours, lower at night)
- Simulated database query latency tracked separately

**Prometheus Metrics Exposed:**
- `http_requests_total{method, path, status}` -- request counter
- `http_request_duration_seconds{method, path}` -- latency histogram
- `user_requests_total{operation}` -- request counter by operation type
- `user_auth_attempts_total{result}` -- authentication outcomes
- `active_sessions` -- current active session count (gauge)
- `cache_hits_total{result}` -- cache hit/miss counter
- `cache_operation_duration_seconds{operation}` -- cache latency histogram
- `user_db_query_duration_seconds` -- database query latency histogram

### 2.4 Load Generator (port 8090)

**Purpose:** Generates realistic, continuous traffic against all three microservices. Produces data that makes the observability dashboards meaningful.

**Behavior:**

**Diurnal Traffic Pattern:**
The load generator models real-world traffic using a cosine function:
```
multiplier = 0.2 + 0.8 * (0.5 + 0.5 * cos((hour - 12) * pi / 12))
```
- Peak traffic at noon (multiplier = 1.0)
- Minimum traffic at 3 AM (multiplier = 0.2)
- This means traffic ranges from 20% to 100% of the base RPS

**Burst Simulation:**
- Each traffic cycle has a 2% probability of triggering a burst
- During a burst, traffic multiplies by 5x for 30 seconds
- Bursts are logged with warnings for easy identification in Loki

**Weighted Endpoint Selection:**
Each service has endpoints with different weights controlling how often they are hit:

| Service | Endpoint | Weight | Frequency |
|---------|----------|--------|-----------|
| order-service | GET /api/orders | 5 | Most common |
| order-service | POST /api/orders | 3 | Moderate |
| order-service | GET /api/orders/ord-001 | 2 | Less common |
| payment-service | POST /api/payments | 5 | Most common |
| payment-service | GET /api/payments | 4 | Common |
| user-service | GET /api/users/usr-100 | 4 | Most common |
| user-service | GET /api/users | 3 | Common |
| user-service | POST /api/users/auth | 3 | Common |

**Prometheus Metrics (self-monitoring):**
- `loadgen_requests_sent_total{service, method, path}` -- requests sent
- `loadgen_responses_received_total{service, status_code}` -- responses received
- `loadgen_request_duration_seconds{service}` -- request latency histogram
- `loadgen_request_errors_total{service, error_type}` -- connection errors
- `loadgen_current_rps{service}` -- current target requests per second (gauge)

### 2.5 Service Communication

```
                  +----------------+
                  | Load Generator |
                  +---+----+----+--+
                      |    |    |
            +---------+    |    +----------+
            |              |               |
            v              v               v
    +-------+------+  +----+-------+  +----+--------+
    | Order Service |  |  Payment   |  | User Service |
    | :8081         |  |  Service   |  | :8083        |
    +-------+-------+  |  :8082    |  +----+---------+
            |           +----------+       ^
            |                              |
            +--- POST /api/payments ------>| (via circuit breaker)
            |                              |
            +--- GET /api/users/validate --+ (via circuit breaker)
```

All inter-service communication happens over HTTP within the Docker `backend` network. Services reference each other by container name (e.g., `http://payment-service:8082`).

---

## 3. Observability Stack

### 3.1 Prometheus

**Version:** v2.51.0

**Configuration file:** `monitoring/prometheus/prometheus.yml`

**Scrape Configuration:**
- Global scrape interval: 15 seconds
- Evaluation interval: 15 seconds (for recording and alerting rules)
- Scrape timeout: 10 seconds per target
- External labels: `cluster=production`, `environment=prod`, `region=us-east-1`

**Scrape Jobs (for Docker Compose / local development, the `application-services` static config is the relevant one):**

| Job Name | Target | Purpose |
|----------|--------|---------|
| `prometheus` | localhost:9090 | Self-monitoring |
| `application-services` | Each microservice's /metrics | Application metrics |
| `node-exporter` | node-exporter:9100 | Host metrics |
| `kubernetes-pods` | Auto-discovered | K8s pod metrics (production) |
| `kubernetes-nodes` | Auto-discovered | Kubelet metrics (production) |
| `kubernetes-cadvisor` | Auto-discovered | Container resource metrics (production) |
| `kubernetes-apiservers` | Auto-discovered | API server metrics (production) |
| `kube-state-metrics` | Auto-discovered | K8s object state (production) |
| `blackbox-http` | External URLs | Synthetic monitoring (production) |

**Storage:**
- TSDB retention: 30 days (`--storage.tsdb.retention.time=30d`)
- Data volume: `prometheus-data` (Docker named volume)
- Admin API and lifecycle management enabled for hot-reloading configuration

**Recording Rules:**
Two rule files are loaded:

1. **`rules/slo-rules.yml`** -- SLO/SLI recording rules and multi-window multi-burn-rate alerts (see section 4)
2. **`rules/application-rules.yml`** -- RED method and USE method pre-computed metrics

**Alert Rules:**
Two alert files are loaded:

1. **`alerts/critical.yml`** -- High error rate, high latency, error budget exhausted, pod crash loops, node not ready, PV full, API server down, etcd failures, target down
2. **`alerts/warning.yml`** -- Elevated error/latency, error budget running low, high CPU/memory, OOM kills, deployment replica mismatches, HPA maxed out, scrape failures

### 3.2 Grafana

**Version:** 10.4.0

**Authentication:** admin/admin (configured via `GF_SECURITY_ADMIN_USER` and `GF_SECURITY_ADMIN_PASSWORD` environment variables). Sign-up is disabled.

**Datasource Provisioning** (`monitoring/grafana/provisioning/datasources/datasources.yml`):

Three datasources are auto-provisioned on startup:

| Datasource | Type | URL | Default |
|------------|------|-----|---------|
| Prometheus | prometheus | http://prometheus:9090 | Yes |
| Loki | loki | http://loki:3100 | No |
| Alertmanager | alertmanager | http://alertmanager:9093 | No |

All datasources are provisioned as non-editable with `access: proxy` (server-side proxying).

**Dashboard Provisioning** (`monitoring/grafana/provisioning/dashboards/dashboards.yml`):

Dashboards are loaded from `/var/lib/grafana/dashboards` (mounted from `monitoring/grafana/dashboards/`) into an "SRE" folder. Dashboards auto-refresh every 30 seconds.

**Dashboards:**

| Dashboard | File | Purpose |
|-----------|------|---------|
| SLO Overview | `slo-overview.json` | Error budget gauges, SLI trends, burn rate over time |
| Service Health | `service-health.json` | RED metrics per service (QPS, error rate, latency percentiles, heatmap) |
| Infrastructure | `infrastructure.json` | Node CPU/memory/disk utilization, network I/O, pod count |

### 3.3 Alertmanager

**Version:** v0.27.0

**Configuration file:** `monitoring/alertmanager/alertmanager.yml`

**Route Tree:**

The route tree processes alerts top-to-bottom with "first match wins" semantics:

```
root (default: slack-warnings)
 |
 +-- severity="critical"  --> pagerduty-critical (continue: true)
 |     group_wait: 10s, repeat: 1h
 |
 +-- severity="critical"  --> slack-critical
 |     group_wait: 10s, repeat: 2h
 |
 +-- severity="warning"   --> slack-warnings
 |     group_wait: 30s, repeat: 4h
 |
 +-- category="slo"       --> slack-slo
 |     group_wait: 1m, repeat: 6h
 |
 +-- team="infrastructure" --> slack-infrastructure
 |     group_wait: 30s, repeat: 4h
 |
 +-- severity="info"      --> email-info
 |     group_wait: 10m, repeat: 24h
 |
 +-- alertname="Watchdog" --> null (discarded)
```

**Grouping:** Alerts are grouped by `alertname`, `service`, and `namespace` to reduce notification noise.

**Receivers:**

| Receiver | Target | When |
|----------|--------|------|
| pagerduty-critical | PagerDuty | Critical alerts -- pages on-call engineer |
| slack-critical | #sre-critical-alerts | Critical alerts -- team visibility |
| slack-warnings | #sre-warnings | Warning alerts -- business hours investigation |
| slack-slo | #sre-slo-alerts | SLO/error budget alerts |
| slack-infrastructure | #sre-infrastructure | Infrastructure alerts |
| email-info | sre-team@example.com | Low-urgency informational alerts |
| null | (discarded) | Watchdog heartbeat alerts |

**Inhibition Rules:**

| If This Is Firing... | ...Then Suppress This | Match On |
|----------------------|----------------------|----------|
| severity=critical | severity=warning | alertname, service, namespace |
| severity=critical | severity=info | service, namespace |
| severity=warning | severity=info | service, namespace |
| NodeNotReady | category=workload | node |
| KubeAPIServerDown | category=workload | (all) |

These rules prevent alert storms. For example, if a node goes down, you get one NodeNotReady alert instead of dozens of pod-level alerts for every pod on that node.

### 3.4 Loki

**Version:** 2.9.5

**Configuration file:** `monitoring/loki/loki-config.yml`

**Architecture:**
- Single-node mode for local development (all components in one process)
- In-memory ring store and KV store (no external dependencies like Consul or etcd)
- Authentication disabled (`auth_enabled: false`)

**Log Ingestion Pipeline:**
```
Docker containers --> Promtail (scrapes /var/run/docker.sock)
                         |
                         v
                    Distributor (validates, rate-limits)
                         |
                         v
                    Ingester (buffers in WAL, builds chunks)
                         |
                         v
                    Filesystem (/loki/chunks)
                         |
                         v
                    Compactor (compacts and applies retention)
```

**Write-Ahead Log (WAL):**
- Enabled for crash recovery
- Directory: `/loki/wal`
- Flushes on shutdown
- Replay memory ceiling: 1 GB

**Schema:**
- TSDB-based index (v13 schema, recommended for production)
- Index prefix: `index_`, period: 24h
- Object store: filesystem (S3 in production -- commented out in config)

**Retention:**
- 31 days (744 hours)
- Compaction interval: 10 minutes
- Delete delay: 2 hours after marking for deletion

**Limits:**
- Ingestion rate: 10 MB/s per tenant, 20 MB burst
- Per-stream rate limit: 5 MB/s, 15 MB burst
- Max query range: 30 days + 1 hour
- Max query parallelism: 32
- Max log line size: 256 KB
- Max streams per user: 10,000

---

## 4. SRE Methodology Deep Dive

### 4.1 SLO/SLI Definitions

This project defines two Service Level Objectives:

| SLO | SLI | Target | Window | Budget |
|-----|-----|--------|--------|--------|
| Availability | Ratio of non-5xx responses to total responses | 99.9% | 30 days | 43.2 minutes of downtime |
| Latency | Ratio of requests completing in < 500ms | 99% | 30 days | 1% of requests may exceed 500ms |

**SLI Recording Rules:**

The SLIs are computed as ratios at multiple time windows (1m, 5m, 30m, 1h, 6h, 3d, 30d) using Prometheus recording rules:

```
Availability SLI = sum(rate(http_requests_total{status_code!~"5.."}[window]))
                   /
                   sum(rate(http_requests_total[window]))

Latency SLI = sum(rate(http_request_duration_seconds_bucket{le="0.5"}[window]))
              /
              sum(rate(http_request_duration_seconds_count[window]))
```

### 4.2 Error Budget Calculation

The error budget represents how much unreliability you can tolerate before violating the SLO.

**Formula:**

```
Error Budget = 1 - SLO Target

For 99.9% availability:
  Error Budget = 1 - 0.999 = 0.001 (0.1%)
  Over 30 days: 0.001 * 30 * 24 * 60 = 43.2 minutes of downtime allowed

Error Budget Remaining = 1 - (actual_error_rate / allowed_error_rate)
                       = 1 - ((1 - actual_availability) / (1 - 0.999))

Example:
  If actual availability = 99.95% (error rate = 0.05%)
  Budget remaining = 1 - (0.0005 / 0.001) = 1 - 0.5 = 50%
  (Half the budget has been consumed)
```

### 4.3 Multi-Window Multi-Burn-Rate Alerting

This project implements the multi-window multi-burn-rate alerting strategy from [Chapter 5 of the Google SRE Workbook](https://sre.google/workbook/alerting-on-slos/). This approach balances detection speed with false positive reduction.

**The Problem with Simple Alerting:**
- A single threshold (e.g., "alert if error rate > 0.1%") is either too sensitive (false positives) or too slow (misses real incidents).
- Using a single time window means you either catch fast burns but get noisy alerts, or catch slow burns but miss urgent ones.

**The Multi-Window Solution:**

Each alert level uses TWO windows: a long window for significance and a short window for recency. Both conditions must be true simultaneously.

| Severity | Long Window | Short Window | Burn Rate | What It Means | Action |
|----------|-------------|--------------|-----------|---------------|--------|
| Page (critical) | 1h | 5m | 14.4x | Consuming 2% of 30-day budget per hour. At this rate, entire budget gone in ~50h. | Immediate response. Wake up on-call. |
| Page (critical) | 6h | 30m | 6x | Consuming 0.8% of budget per hour. At this rate, entire budget gone in ~5 days. | Urgent response during business hours. |
| Ticket (warning) | 3d | 6h | 1x | Consuming budget at exactly the sustainable rate. | Create ticket, investigate soon. |

**How Burn Rate Is Calculated:**

```
Burn Rate = (1 - actual_availability_over_window) / (1 - SLO_target)

For 99.9% SLO:
  If 1h availability = 98.56%:
    burn rate = (1 - 0.9856) / (1 - 0.999) = 0.0144 / 0.001 = 14.4x

The alert expression for a 14.4x burn rate check:
  slo:availability:ratio_rate1h < (1 - 14.4 * (1 - 0.999))
  = slo:availability:ratio_rate1h < (1 - 14.4 * 0.001)
  = slo:availability:ratio_rate1h < (1 - 0.0144)
  = slo:availability:ratio_rate1h < 0.9856
```

**Example Scenario:**

A deployment introduces a bug causing 5% errors on the order-service:

1. **Minute 1-2:** Short window (5m) detects the spike. Long window (1h) still looks fine because of historical good data. Alert does NOT fire (prevents false positive from a brief blip).
2. **Minute 5:** Both the 5m AND 1h windows show a burn rate > 14.4x. The critical alert fires. PagerDuty pages the on-call engineer.
3. **The fix is deployed at minute 15.** Error rate returns to normal. Within 5 minutes, the short window recovers. Within an hour, the long window recovers. The alert resolves.

### 4.4 RED Method

The RED method (Rate, Errors, Duration) is applied to every microservice. It answers: "Is this service working correctly right now?"

| Signal | Metric | Recording Rule | What to Watch |
|--------|--------|----------------|---------------|
| **R**ate | `http_requests_total` | `app:http_requests:rate5m` | Sudden drops (upstream failure) or spikes (DDoS/burst) |
| **E**rrors | `http_requests_total{status=~"5.."}` | `app:http_errors:ratio5m` | Error percentage exceeding SLO target |
| **D**uration | `http_request_duration_seconds` | `app:http_request_duration:p99_5m` | p99 latency exceeding 500ms threshold |

The `application-rules.yml` file pre-computes these at multiple time windows (1m, 5m, 15m, 1h) and multiple granularities (by service, by method, by status class, by endpoint).

### 4.5 USE Method

The USE method (Utilization, Saturation, Errors) is applied to infrastructure resources. It answers: "Is the infrastructure under pressure?"

| Signal | Resource | Metric | Threshold |
|--------|----------|--------|-----------|
| **U**tilization | CPU | `rate(container_cpu_usage_seconds_total[5m])` / requested CPU | Warning > 80% |
| **U**tilization | Memory | `container_memory_working_set_bytes` / requested memory | Warning > 80% |
| **S**aturation | CPU | CPU throttling periods | Indicates resource constraints |
| **S**aturation | Memory | OOM kills (`kube_pod_container_status_last_terminated_reason{reason="OOMKilled"}`) | Any OOM kill is noteworthy |
| **E**rrors | Node | `kube_node_status_condition{condition="Ready"}` | Node not ready |
| **E**rrors | Disk | `kube_node_status_condition{condition="DiskPressure"}` | Disk pressure |

---

## 5. Terraform Modules

The Terraform code provisions AWS infrastructure for the production deployment of the observability stack.

### 5.1 Networking Module (`terraform/modules/networking/`)

Creates the VPC and network topology for the EKS cluster:

| Resource | Purpose |
|----------|---------|
| VPC | Isolated network with DNS support (`10.0.0.0/16` for dev, `10.1.0.0/16` for prod) |
| 3 Public Subnets | One per AZ, with auto-assign public IP, tagged for EKS external load balancers |
| 3 Private Subnets | One per AZ, tagged for EKS internal load balancers |
| Internet Gateway | Outbound internet access for public subnets |
| NAT Gateway | Outbound internet access for private subnets (EKS worker nodes) |
| Route Tables | Public routes through IGW, private routes through NAT |

### 5.2 Monitoring Module (`terraform/modules/monitoring/`)

Creates AWS resources that the observability stack depends on:

| Resource | Purpose |
|----------|---------|
| KMS Key | Encrypts monitoring data at rest (S3 buckets, SNS topics). Key rotation enabled. |
| S3 Bucket (Loki) | `{project}-{env}-loki-chunks` -- stores Loki log chunks. Versioning and KMS encryption enabled. `force_destroy` enabled for non-prod. |
| S3 Bucket (Thanos) | `{project}-{env}-thanos-store` -- stores Prometheus long-term metrics via Thanos sidecar. |
| SNS Topic | `{project}-{env}-alerts` -- alert routing from Alertmanager. KMS-encrypted. Optional email subscription. |
| IAM Role (Prometheus) | IRSA role for the Prometheus service account. Grants S3 read/write to the Thanos bucket. |
| IAM Role (Loki) | IRSA role for the Loki service account. Grants S3 read/write to the Loki bucket and KMS decrypt. |
| CloudWatch Log Group | `/sre-platform/{env}/monitoring` -- CloudWatch logs for the monitoring stack itself. 90-day retention for prod, 14-day for dev. |

**IRSA (IAM Roles for Service Accounts):**

IRSA allows Kubernetes pods to assume IAM roles without storing credentials. The trust policy uses the EKS OIDC provider to verify the Kubernetes service account identity:

```
Pod (service account: monitoring/prometheus)
  --> AssumeRoleWithWebIdentity (verified by OIDC provider)
  --> IAM Role: sre-platform-dev-prometheus
  --> S3 access to Thanos bucket
```

### 5.3 Environments

| Environment | VPC CIDR | State File | Notes |
|-------------|----------|------------|-------|
| dev | 10.0.0.0/16 | `environments/dev/terraform.tfstate` | `force_destroy` enabled on S3 buckets, shorter log retention |
| prod | 10.1.0.0/16 | `environments/prod/terraform.tfstate` | `force_destroy` disabled, 90-day log retention |

Both environments use:
- Terraform >= 1.5.0
- AWS provider ~> 5.40
- S3 backend with DynamoDB state locking
- Default tags: `Project`, `Environment`, `ManagedBy`

---

## 6. Kubernetes Deployment

### 6.1 Kustomize Structure

```
kubernetes/
  base/                    # Base manifests (shared across environments)
    kustomization.yaml     # Lists all resources
    namespace.yaml         # Creates "monitoring" namespace
    prometheus.yaml        # Prometheus Deployment + Service + ConfigMap
    grafana.yaml           # Grafana Deployment + Service + ConfigMap
    alertmanager.yaml      # Alertmanager StatefulSet + Service + ConfigMap
    loki.yaml              # Loki StatefulSet + Service + ConfigMap
  overlays/
    dev/
      kustomization.yaml   # Extends base, adds "environment: dev" label,
                           # reduces Prometheus memory to 256Mi-1Gi
    prod/
      kustomization.yaml   # Extends base, adds "environment: production" label,
                           # increases Prometheus to 500m CPU / 1-4Gi memory,
                           # scales Grafana to 2 replicas,
                           # scales Alertmanager to 3 replicas
```

### 6.2 Resource Sizing by Environment

| Component | Dev | Prod |
|-----------|-----|------|
| Prometheus Memory | 256Mi request / 1Gi limit | 1Gi request / 4Gi limit |
| Prometheus CPU | default | 500m request / 2 cores limit |
| Grafana Replicas | 1 | 2 |
| Alertmanager Replicas | 1 | 3 (cluster for HA) |

### 6.3 Deployment Workflow

```bash
# Apply the dev overlay
kubectl apply -k kubernetes/overlays/dev/

# Apply the prod overlay
kubectl apply -k kubernetes/overlays/prod/
```

Kustomize merges the base manifests with the overlay patches, applying environment-specific resource limits, labels, and replica counts.

---

## 7. CI/CD Pipeline

### 7.1 CI Pipeline (`.github/workflows/ci.yaml`)

Triggers on: pull requests to `main` or `develop`, pushes to `develop`.

```
+-----------+     +-------------------+     +-------------------+
| Lint &    |     | Build Docker      |     | Validate          |
| Test      |---->| Images            |     | Monitoring Configs|
|           |     |                   |     |                   |
| (matrix:  |     | (matrix:          |     | promtool check    |
|  4 svc)   |     |  4 services)      |     | config            |
|           |     |                   |     | promtool check    |
| golangci  |     | docker build      |     | rules             |
| go test   |     | trivy scan        |     | yamllint          |
|  -race    |     | (CRITICAL/HIGH)   |     |                   |
+-----------+     +-------------------+     +-------------------+
```

**Steps in detail:**

1. **Lint & Test** (runs in parallel for each of the 4 services):
   - Sets up Go 1.22
   - Runs `golangci-lint` for static analysis
   - Runs `go test -v -race -coverprofile=coverage.out ./...` for tests with race detection

2. **Build** (depends on Lint & Test passing):
   - Builds Docker image tagged with the commit SHA
   - Scans with Trivy for CRITICAL and HIGH vulnerabilities (exit code 0 = report only, does not block)

3. **Validate Configs** (runs in parallel with other jobs):
   - `promtool check config` validates Prometheus configuration syntax
   - `promtool check rules` validates all recording and alerting rules
   - `yamllint` validates YAML syntax across monitoring/ and kubernetes/ directories

### 7.2 CD Pipeline (`.github/workflows/cd.yaml`)

Triggers on: pushes to `main`, tag creation matching `v*`.

```
+-------------------+     +-------------------+
| Build & Push      |---->| Deploy to K8s     |
| to ECR            |     |                   |
|                   |     | aws eks update-   |
| (matrix:          |     |   kubeconfig      |
|  4 services)      |     | kubectl apply -k  |
|                   |     |   overlays/dev/   |
| OIDC auth         |     | kubectl rollout   |
| ECR login         |     |   status          |
| docker push :sha  |     |                   |
| docker push :latest|    |                   |
+-------------------+     +-------------------+
```

**Steps in detail:**

1. **Build & Push** (runs in parallel for each of the 4 services):
   - Authenticates to AWS using OIDC federation (no static credentials)
   - Logs into Amazon ECR
   - Builds and pushes images tagged with both the commit SHA and `latest`

2. **Deploy** (depends on Build & Push completing):
   - Configures `kubectl` to point at the EKS cluster
   - Applies the Kustomize overlay: `kubectl apply -k kubernetes/overlays/dev/`
   - Verifies deployments reach ready state: `kubectl rollout status` with a 5-minute timeout

---

## 8. Data Flow

### 8.1 Request to Alert Flow

```
User Request
     |
     v
Microservice (handles request, records metrics)
     |
     +--> http_requests_total{method, path, status}  counter++
     +--> http_request_duration_seconds{method, path}  histogram.Observe()
     +--> (service-specific metrics like orders_created_total)
     |
     |  [every 15 seconds]
     v
Prometheus Scrape (/metrics endpoint)
     |
     v
Prometheus TSDB (stores raw time series)
     |
     |  [every 15-30 seconds]
     v
Recording Rules Evaluate
     |
     +--> slo:availability:ratio_rate{1m,5m,30m,1h,6h,3d,30d}
     +--> slo:latency:ratio_rate{5m,1h,6h,3d,30d}
     +--> slo:error_budget:availability_remaining
     +--> slo:error_budget:availability_burn_rate{1h,6h,3d}
     +--> app:http_requests:rate5m (RED: Rate)
     +--> app:http_errors:ratio5m  (RED: Errors)
     +--> app:http_request_duration:p99_5m (RED: Duration)
     |
     |  [every 15 seconds]
     v
Alert Rules Evaluate
     |
     +--> ErrorBudgetBurnRateCritical  (14.4x or 6x burn)
     +--> ErrorBudgetBurnRateWarning   (1x burn over 3d)
     +--> HighErrorRate                (>1% over 5m)
     +--> HighLatency                  (p99 > 1s over 5m)
     +--> ErrorBudgetExhausted         (remaining < 0)
     |
     |  [if alert condition is true for `for` duration]
     v
Prometheus sends alert to Alertmanager
     |
     v
Alertmanager Route Tree
     |
     +--> severity=critical --> PagerDuty (page on-call, 10s group_wait)
     |                     --> Slack #sre-critical-alerts
     |
     +--> severity=warning  --> Slack #sre-warnings (30s group_wait)
     |
     +--> category=slo      --> Slack #sre-slo-alerts (1m group_wait)
     |
     +--> severity=info     --> Email (10m group_wait, 24h repeat)
```

### 8.2 Log Flow

```
Microservice (writes structured JSON logs to stdout)
     |
     v
Docker logging driver (captures container stdout/stderr)
     |
     v
Promtail (reads from /var/run/docker.sock)
     |
     +--> Extracts labels: container name, compose service
     +--> Parses structured JSON fields
     |
     v
Loki Distributor (validates, rate-limits, hashes tenant)
     |
     v
Loki Ingester (buffers in WAL, builds compressed chunks)
     |
     v
Loki Filesystem / S3 (persistent storage)
     |
     v
Grafana Explore (queries via LogQL)
     e.g., {container="order-service"} |= "error"
```

### 8.3 Dashboard Rendering Flow

```
User opens Grafana dashboard
     |
     v
Grafana reads dashboard JSON (provisioned from filesystem)
     |
     v
Dashboard panels issue PromQL queries to Prometheus datasource
     |
     +--> Panel "Error Budget": query slo:error_budget:availability_remaining
     +--> Panel "Request Rate": query app:http_requests:rate5m
     +--> Panel "p99 Latency":  query app:http_request_duration:p99_5m
     |
     v
Prometheus evaluates queries against TSDB
     |
     +--> Most queries hit pre-computed recording rules (fast)
     +--> Recording rules were evaluated on the 15-30s cycle
     |
     v
Results returned to Grafana --> Rendered as graphs, gauges, tables
```
