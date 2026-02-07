# SRE Observability Platform

A production-grade observability and SRE platform implementing Google's SRE principles with SLO/SLI monitoring, error budget tracking, multi-signal observability, and automated incident response.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                     Microservices Layer                             │
│  ┌──────────────┐  ┌──────────────────┐  ┌───────────────┐         │
│  │ Order Service │  │ Payment Service  │  │ User Service  │         │
│  │  /metrics     │  │  /metrics        │  │  /metrics     │         │
│  │  ~2% errors   │  │  ~5% errors      │  │  ~0.1% errors │         │
│  └──────┬───────┘  └───────┬──────────┘  └───────┬───────┘         │
│         │                  │                     │                  │
│  ┌──────┴──────────────────┴─────────────────────┘                  │
│  │              Load Generator (diurnal traffic)                    │
│  └──────────────────────────────────────────────────────────────────│
└─────────┬───────────────────┬────────────────────┬──────────────────┘
          │ metrics           │ logs               │ traces
          ▼                   ▼                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                    Observability Stack                              │
│                                                                     │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────────┐  │
│  │  Prometheus   │  │    Loki      │  │     Alertmanager         │  │
│  │              │  │              │  │                          │  │
│  │ SLO Rules    │  │ Log Queries  │  │  Critical → PagerDuty   │  │
│  │ Recording    │  │ Log Alerts   │  │  Warning  → Slack       │  │
│  │ Burn Rate    │  │              │  │  Info     → Email       │  │
│  └──────┬───────┘  └──────┬───────┘  └──────────────────────────┘  │
│         │                 │                                         │
│         └────────┬────────┘                                         │
│                  ▼                                                  │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │                      Grafana                                 │   │
│  │  ┌──────────────┐ ┌────────────────┐ ┌───────────────────┐  │   │
│  │  │ SLO Overview │ │ Service Health │ │ Infrastructure    │  │   │
│  │  │              │ │                │ │                   │  │   │
│  │  │ Error Budget │ │ RED Metrics    │ │ Node Resources    │  │   │
│  │  │ Burn Rate    │ │ Latency Heat   │ │ Cluster Capacity  │  │   │
│  │  │ SLI Trends   │ │ Error Rates    │ │ Network I/O       │  │   │
│  │  └──────────────┘ └────────────────┘ └───────────────────┘  │   │
│  └──────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────┘
```

## Project Structure

```
sre-observability-platform/
├── microservices/
│   ├── order-service/            # Go service with Prometheus metrics
│   ├── payment-service/          # Go service with payment simulation
│   ├── user-service/             # Go service with auth metrics
│   └── load-generator/           # Traffic generator (diurnal patterns)
├── monitoring/
│   ├── prometheus/
│   │   ├── prometheus.yml        # Scrape configs & service discovery
│   │   ├── rules/
│   │   │   ├── slo-rules.yml     # SLO/SLI recording rules
│   │   │   └── application-rules.yml
│   │   └── alerts/
│   │       ├── critical.yml      # Error budget burn rate alerts
│   │       └── warning.yml       # Capacity & degradation alerts
│   ├── grafana/
│   │   ├── dashboards/
│   │   │   ├── slo-overview.json       # Error budget & SLI tracking
│   │   │   ├── service-health.json     # RED method per service
│   │   │   └── infrastructure.json     # Cluster & node metrics
│   │   └── provisioning/
│   ├── alertmanager/
│   │   └── alertmanager.yml      # Route tree & receivers
│   └── loki/
│       └── loki-config.yml       # Log aggregation config
├── terraform/
│   ├── modules/
│   │   ├── networking/           # VPC for monitoring infra
│   │   ├── eks/                  # EKS cluster
│   │   └── monitoring/           # S3, IAM, SNS for monitoring
│   └── environments/
│       ├── dev/
│       └── prod/
├── kubernetes/
│   ├── base/                     # K8s manifests for monitoring stack
│   └── overlays/
├── .github/workflows/
│   ├── ci.yaml                   # Lint, test, build, scan
│   └── cd.yaml                   # Deploy monitoring stack
├── docker-compose.yml            # Local development stack
└── scripts/
    ├── setup-local.sh            # One-command local setup
    ├── generate-traffic.sh       # Start load generator
    └── cleanup.sh                # Tear down
```

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

## Quick Start

### Local Development (Docker Compose)

```bash
# Start the full stack
./scripts/setup-local.sh

# Or manually:
docker-compose up -d

# Access dashboards
# Grafana:      http://localhost:3000 (admin/admin)
# Prometheus:   http://localhost:9090
# Alertmanager: http://localhost:9093

# Generate traffic
./scripts/generate-traffic.sh

# Watch SLO dashboards in Grafana as traffic flows
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

## Technologies

| Category | Tools |
|----------|-------|
| Metrics | Prometheus |
| Dashboards | Grafana |
| Logs | Loki + Promtail |
| Alerting | Alertmanager |
| Applications | Go microservices |
| IaC | Terraform |
| Orchestration | Kubernetes (EKS) |
| CI/CD | GitHub Actions |
| Local Dev | Docker Compose |
| Cloud | AWS (EKS, S3, IAM, SNS, KMS) |

## Key SRE References

- [Google SRE Book](https://sre.google/sre-book/table-of-contents/)
- [Google SRE Workbook](https://sre.google/workbook/table-of-contents/)
- [Alerting on SLOs](https://sre.google/workbook/alerting-on-slos/)
- [Prometheus Best Practices](https://prometheus.io/docs/practices/)

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
