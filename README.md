# SRE Observability Platform

A production-grade observability and SRE platform implementing Google's SRE principles with SLO/SLI monitoring, error budget tracking, multi-signal observability, and automated incident response.

---

**New here? Start with the [Getting Started Guide](docs/GETTING-STARTED.md)** -- it walks you through the full local setup in about 10 minutes.

---

## Run It in 60 Seconds

```bash
git clone https://github.com/<your-username>/sre-observability-platform.git
cd sre-observability-platform
docker compose up -d
```

Then open [http://localhost:3000](http://localhost:3000) and log in with `admin` / `admin`. Navigate to **Dashboards > SRE** to see SLO Overview, Service Health, and Infrastructure dashboards populate with live data within 2-3 minutes.

---

## Documentation

| Document | Description |
|----------|-------------|
| [Getting Started Guide](docs/GETTING-STARTED.md) | Prerequisites, step-by-step local setup, dashboard walkthrough |
| [Architecture](docs/ARCHITECTURE.md) | Deep technical architecture, SRE methodology, data flows |
| [Troubleshooting](docs/TROUBLESHOOTING.md) | Common issues and their solutions with exact commands |

---

## What You'll Learn

This project demonstrates real-world SRE and DevOps skills:

| Skill | Where to See It |
|-------|----------------|
| **SLO/SLI monitoring and error budget tracking** | `monitoring/prometheus/rules/slo-rules.yml`, SLO Overview dashboard |
| **Multi-window multi-burn-rate alerting** (Google SRE Book) | `monitoring/prometheus/rules/slo-rules.yml` (alert rules section) |
| **RED method** (Rate, Errors, Duration) observability | `monitoring/prometheus/rules/application-rules.yml`, Service Health dashboard |
| **USE method** (Utilization, Saturation, Errors) monitoring | `monitoring/prometheus/rules/application-rules.yml` (saturation section), Infrastructure dashboard |
| **Prometheus recording and alerting rules** | `monitoring/prometheus/rules/`, `monitoring/prometheus/alerts/` |
| **Grafana dashboard design and provisioning** | `monitoring/grafana/dashboards/`, `monitoring/grafana/provisioning/` |
| **Alertmanager routing, grouping, and inhibition** | `monitoring/alertmanager/alertmanager.yml` |
| **Circuit breaker pattern in distributed systems** | `microservices/order-service/main.go`, `microservices/payment-service/main.go` |
| **Go microservice development with Prometheus instrumentation** | `microservices/*/main.go` |
| **Docker multi-stage builds** | `microservices/*/Dockerfile` |
| **Kubernetes deployment with Kustomize overlays** | `kubernetes/base/`, `kubernetes/overlays/dev/`, `kubernetes/overlays/prod/` |
| **Terraform IaC for AWS monitoring infrastructure** | `terraform/modules/networking/`, `terraform/modules/monitoring/` |
| **CI/CD pipelines with security scanning** | `.github/workflows/ci.yaml`, `.github/workflows/cd.yaml` |
| **Log aggregation with Loki** | `monitoring/loki/loki-config.yml` |

---

## Architecture

```
+---------------------------------------------------------------------------+
|                     Microservices Layer                                     |
|  +----------------+  +------------------+  +---------------+              |
|  | Order Service  |  | Payment Service  |  | User Service  |              |
|  | :8081          |  | :8082            |  | :8083         |              |
|  | /metrics       |  | /metrics         |  | /metrics      |              |
|  | ~2% errors     |  | ~5% errors       |  | ~0.1% errors  |              |
|  +------+---------+  +-------+----------+  +-------+-------+              |
|         |                    |                     |                       |
|  +------+--------------------+---------------------+                      |
|  |              Load Generator (:8090)                                    |
|  |        Diurnal traffic, burst simulation                               |
|  +--------------------------------------------------------------------+   |
+---------+-----------------------+-----------------------+-----------------+
          | metrics               | logs                  | traces
          v                       v                       v
+---------------------------------------------------------------------------+
|                    Observability Stack                                      |
|                                                                            |
|  +--------------+   +--------------+   +--------------------------+       |
|  |  Prometheus  |   |    Loki      |   |     Alertmanager         |       |
|  |  :9090       |   |    :3100     |   |     :9093                |       |
|  |              |   |              |   |                          |       |
|  | SLO Rules   |   | Log Queries  |   |  Critical -> PagerDuty  |       |
|  | Recording   |   | Log Alerts   |   |  Warning  -> Slack      |       |
|  | Burn Rate   |   |              |   |  Info     -> Email      |       |
|  +------+------+   +------+-------+   +--------------------------+       |
|         |                 |                                               |
|         +--------+--------+                                               |
|                  v                                                         |
|  +----------------------------------------------------------------+      |
|  |                      Grafana (:3000)                            |      |
|  |  +----------------+ +------------------+ +-------------------+  |      |
|  |  | SLO Overview   | | Service Health   | | Infrastructure    |  |      |
|  |  |                | |                  | |                   |  |      |
|  |  | Error Budget   | | RED Metrics      | | Node Resources    |  |      |
|  |  | Burn Rate      | | Latency Heatmap  | | Cluster Capacity  |  |      |
|  |  | SLI Trends     | | Error Rates      | | Network I/O       |  |      |
|  |  +----------------+ +------------------+ +-------------------+  |      |
|  +----------------------------------------------------------------+      |
+---------------------------------------------------------------------------+
```

---

## Quick Start

### Local Development (Docker Compose)

**Prerequisites:** Docker Desktop with at least 4 CPUs and 4 GB RAM allocated. See the [Getting Started Guide](docs/GETTING-STARTED.md) for detailed installation instructions.

```bash
# Start the full stack
./scripts/setup-local.sh

# Or manually:
docker compose up -d

# Verify all services are running
docker compose ps
```

Expected output from `docker compose ps` (after ~60 seconds):

```
NAME              STATUS                   PORTS
alertmanager      Up (healthy)             0.0.0.0:9093->9093/tcp
grafana           Up (healthy)             0.0.0.0:3000->3000/tcp
loki              Up (healthy)             0.0.0.0:3100->3100/tcp
load-generator    Up                       0.0.0.0:8090->8090/tcp
node-exporter     Up                       0.0.0.0:9100->9100/tcp
order-service     Up (healthy)             0.0.0.0:8081->8081/tcp
payment-service   Up (healthy)             0.0.0.0:8082->8082/tcp
prometheus        Up (healthy)             0.0.0.0:9090->9090/tcp
promtail          Up
user-service      Up (healthy)             0.0.0.0:8083->8083/tcp
```

**Access the UIs:**

| UI | URL | Credentials |
|----|-----|-------------|
| Grafana | [http://localhost:3000](http://localhost:3000) | admin / admin |
| Prometheus | [http://localhost:9090](http://localhost:9090) | -- |
| Alertmanager | [http://localhost:9093](http://localhost:9093) | -- |

**Generate traffic and watch dashboards:**

```bash
# Traffic is generated automatically by the load-generator service.
# To manually test individual endpoints:
curl http://localhost:8081/api/orders | jq .
curl -X POST http://localhost:8081/api/orders | jq .
curl http://localhost:8082/api/payments | jq .
curl http://localhost:8083/api/users | jq .
```

**Explore Prometheus queries:**

Open [http://localhost:9090](http://localhost:9090) and try:

```promql
# See all targets and their status
up

# Request rate per service
rate(http_requests_total[5m])

# p99 latency
histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))

# Error budget burn rate
slo:error_budget:availability_burn_rate1h
```

**View logs:**

```bash
docker compose logs -f order-service
docker compose logs -f payment-service
docker compose logs -f user-service
```

**Clean up:**

```bash
./scripts/cleanup.sh
```

### AWS Deployment

```bash
# 1. Configure AWS credentials
export AWS_PROFILE=your-profile

# 2. Deploy infrastructure
cd terraform/environments/dev
terraform init && terraform apply

# 3. Configure kubectl
aws eks update-kubeconfig --name sre-platform-dev --region us-east-1

# 4. Deploy monitoring stack
kubectl apply -k kubernetes/overlays/dev/

# 5. Deploy microservices
kubectl apply -f kubernetes/base/
```

---

## Local vs Cloud

| Aspect | Local (Docker Compose) | Cloud (AWS EKS) |
|--------|----------------------|-----------------|
| **Setup time** | 5 minutes | 30-60 minutes |
| **Cost** | Free | ~$150/month (EKS + EC2 + S3) |
| **Storage** | Docker volumes (local disk) | S3 (Loki chunks, Thanos metrics) |
| **Encryption** | None | KMS-encrypted S3 buckets |
| **IAM** | Not applicable | IRSA for Prometheus and Loki |
| **Scaling** | Single instance | Multiple replicas, HPA |
| **Alerting receivers** | Configured but no real endpoints | PagerDuty, Slack, email |
| **Service discovery** | Static Docker Compose hostnames | Kubernetes service discovery |
| **Use case** | Development, learning, testing | Production monitoring |

---

## API Reference

### Order Service (port 8081)

| Method | Endpoint | Description | Example |
|--------|----------|-------------|---------|
| GET | `/api/orders` | List all orders | `curl http://localhost:8081/api/orders` |
| POST | `/api/orders` | Create a new order (calls user-service and payment-service) | `curl -X POST http://localhost:8081/api/orders` |
| GET | `/api/orders/{orderID}` | Get a specific order | `curl http://localhost:8081/api/orders/ord-001` |
| GET | `/healthz` | Liveness check | `curl http://localhost:8081/healthz` |
| GET | `/readyz` | Readiness check | `curl http://localhost:8081/readyz` |
| GET | `/metrics` | Prometheus metrics | `curl http://localhost:8081/metrics` |

### Payment Service (port 8082)

| Method | Endpoint | Description | Example |
|--------|----------|-------------|---------|
| GET | `/api/payments` | List all payments | `curl http://localhost:8082/api/payments` |
| POST | `/api/payments` | Process a new payment | `curl -X POST http://localhost:8082/api/payments` |
| GET | `/api/payments/{paymentID}` | Get a specific payment | `curl http://localhost:8082/api/payments/pay-001` |
| GET | `/healthz` | Liveness check | `curl http://localhost:8082/healthz` |
| GET | `/readyz` | Readiness check | `curl http://localhost:8082/readyz` |
| GET | `/metrics` | Prometheus metrics | `curl http://localhost:8082/metrics` |

### User Service (port 8083)

| Method | Endpoint | Description | Example |
|--------|----------|-------------|---------|
| GET | `/api/users` | List all users | `curl http://localhost:8083/api/users` |
| POST | `/api/users` | Create a new user | `curl -X POST http://localhost:8083/api/users` |
| GET | `/api/users/validate` | Validate a user (used by order-service) | `curl http://localhost:8083/api/users/validate` |
| GET | `/api/users/{userID}` | Get a specific user (cache-aside) | `curl http://localhost:8083/api/users/usr-100` |
| POST | `/api/users/auth` | Authenticate a user | `curl -X POST http://localhost:8083/api/users/auth` |
| GET | `/healthz` | Liveness check | `curl http://localhost:8083/healthz` |
| GET | `/readyz` | Readiness check | `curl http://localhost:8083/readyz` |
| GET | `/metrics` | Prometheus metrics | `curl http://localhost:8083/metrics` |

### Load Generator (port 8090)

| Method | Endpoint | Description | Example |
|--------|----------|-------------|---------|
| GET | `/healthz` | Liveness check | `curl http://localhost:8090/healthz` |
| GET | `/metrics` | Load generator's own Prometheus metrics | `curl http://localhost:8090/metrics` |

---

## Project Structure

```
sre-observability-platform/
├── microservices/
│   ├── order-service/            # Go service with Prometheus metrics & circuit breakers
│   ├── payment-service/          # Go service with payment type simulation
│   ├── user-service/             # Go service with cache metrics & auth simulation
│   └── load-generator/           # Traffic generator (diurnal patterns, burst mode)
├── monitoring/
│   ├── prometheus/
│   │   ├── prometheus.yml        # Scrape configs & service discovery
│   │   ├── rules/
│   │   │   ├── slo-rules.yml     # SLO/SLI recording rules & burn rate alerts
│   │   │   └── application-rules.yml  # RED & USE method recording rules
│   │   └── alerts/
│   │       ├── critical.yml      # Error budget, error rate, latency, K8s critical alerts
│   │       └── warning.yml       # Capacity, degradation, resource warnings
│   ├── grafana/
│   │   ├── dashboards/
│   │   │   ├── slo-overview.json       # Error budget & SLI tracking
│   │   │   ├── service-health.json     # RED method per service
│   │   │   └── infrastructure.json     # Cluster & node metrics
│   │   └── provisioning/
│   │       ├── datasources/datasources.yml  # Prometheus, Loki, Alertmanager
│   │       └── dashboards/dashboards.yml    # Auto-load dashboards from filesystem
│   ├── alertmanager/
│   │   └── alertmanager.yml      # Route tree, receivers, inhibition rules
│   └── loki/
│       └── loki-config.yml       # Log aggregation with WAL, TSDB schema, retention
├── terraform/
│   ├── modules/
│   │   ├── networking/           # VPC, subnets, NAT gateway, route tables
│   │   └── monitoring/           # S3, KMS, IAM (IRSA), SNS, CloudWatch
│   └── environments/
│       ├── dev/                  # Dev environment (10.0.0.0/16)
│       └── prod/                 # Prod environment (10.1.0.0/16)
├── kubernetes/
│   ├── base/                     # K8s manifests for monitoring stack
│   └── overlays/
│       ├── dev/                  # Dev: reduced resources
│       └── prod/                 # Prod: more replicas, higher resource limits
├── .github/workflows/
│   ├── ci.yaml                   # Lint, test, build, scan, validate configs
│   └── cd.yaml                   # Build, push to ECR, deploy to K8s
├── docker-compose.yml            # Local development stack (10 services)
├── scripts/
│   ├── setup-local.sh            # One-command local setup with health checks
│   ├── generate-traffic.sh       # Start load generator
│   └── cleanup.sh                # Tear down everything
└── docs/
    ├── GETTING-STARTED.md        # Beginner-friendly setup guide
    ├── ARCHITECTURE.md           # Deep technical architecture document
    └── TROUBLESHOOTING.md        # Common issues and solutions
```

---

## SRE Concepts Demonstrated

### SLOs & SLIs

- **Availability SLO**: 99.9% of requests return non-5xx responses (30-day window)
- **Latency SLO**: 99% of requests complete in < 500ms (30-day window)
- **Error Budget**: Calculated as `1 - (1 - SLO)` remaining budget over rolling window

### Multi-Window Multi-Burn-Rate Alerting

Based on [Google SRE Workbook Chapter 5](https://sre.google/workbook/alerting-on-slos/):

| Severity | Long Window | Short Window | Burn Rate | Action |
|----------|------------|--------------|-----------|--------|
| Page | 1h | 5m | 14.4x | Immediate response |
| Page | 6h | 30m | 6x | Urgent response |
| Ticket | 3d | 6h | 1x | Investigate soon |

### RED Method (Rate, Errors, Duration)

Every service exposes:
- **Rate**: Requests per second
- **Errors**: Error percentage
- **Duration**: Latency distribution (p50, p90, p95, p99)

### USE Method (Utilization, Saturation, Errors)

Infrastructure metrics track:
- **Utilization**: CPU, memory, disk usage
- **Saturation**: Queue depth, scheduling delays
- **Errors**: Hardware errors, OOM kills

---

## Dashboards

### SLO Overview

- Error budget remaining (gauge with color thresholds)
- 30-day rolling availability SLI
- 30-day rolling latency SLI
- Burn rate over time
- SLO compliance table across all services

### Service Health

- Request rate (QPS) per service
- Error rate with SLO threshold line
- Latency heatmap
- Pod status and restart tracking

### Infrastructure

- Node CPU/Memory/Disk utilization
- Network I/O throughput
- Pod count by namespace
- Cluster capacity planning

---

## Technologies

| Category | Tools |
|----------|-------|
| Metrics | Prometheus v2.51.0 |
| Dashboards | Grafana 10.4.0 |
| Logs | Loki 2.9.5 + Promtail 2.9.5 |
| Alerting | Alertmanager v0.27.0 |
| Applications | Go 1.22 microservices |
| IaC | Terraform >= 1.5.0 |
| Orchestration | Kubernetes (EKS) with Kustomize |
| CI/CD | GitHub Actions |
| Local Dev | Docker Compose v2 |
| Cloud | AWS (EKS, S3, IAM, SNS, KMS) |
| Host Metrics | Node Exporter v1.7.0 |

---

## Key SRE References

- [Google SRE Book](https://sre.google/sre-book/table-of-contents/)
- [Google SRE Workbook](https://sre.google/workbook/table-of-contents/)
- [Alerting on SLOs](https://sre.google/workbook/alerting-on-slos/)
- [Prometheus Best Practices](https://prometheus.io/docs/practices/)
- [RED Method](https://www.weave.works/blog/the-red-method-key-metrics-for-microservices-architecture/)
- [USE Method](https://www.brendangregg.com/usemethod.html)

---

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
