# Getting Started Guide

This guide walks you through setting up the SRE Observability Platform on your local machine. By the end, you will have a fully functioning observability stack with microservices, Prometheus, Grafana, Alertmanager, and Loki -- all running locally via Docker Compose.

No cloud account or Kubernetes cluster is required for local development.

---

## Table of Contents

1. [Prerequisites](#1-prerequisites)
2. [Verifying Your Tools](#2-verifying-your-tools)
3. [Step-by-Step Local Setup](#3-step-by-step-local-setup)
4. [Exploring the Dashboards](#4-exploring-the-dashboards)
5. [Stopping and Cleaning Up](#5-stopping-and-cleaning-up)
6. [What to Explore Next](#6-what-to-explore-next)

---

## 1. Prerequisites

### Required Tools

#### Docker Desktop

Docker Desktop provides both the Docker engine and Docker Compose v2. You must allocate **at least 4 CPUs and 4 GB of RAM** to Docker for the full stack to run smoothly.

**macOS (Homebrew):**

```bash
brew install --cask docker
```

After installation, open Docker Desktop from your Applications folder. Go to **Settings > Resources** and set:
- CPUs: 4 (minimum)
- Memory: 4 GB (minimum, 6 GB recommended)
- Disk: 20 GB (minimum)

**Linux (Ubuntu/Debian):**

```bash
# Remove old versions
sudo apt-get remove docker docker-engine docker.io containerd runc

# Install prerequisites
sudo apt-get update
sudo apt-get install -y ca-certificates curl gnupg lsb-release

# Add Docker's official GPG key
sudo mkdir -p /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg

# Set up the repository
echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \
  $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

# Install Docker Engine and Compose
sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

# Allow running Docker without sudo
sudo usermod -aG docker $USER
newgrp docker
```

#### Docker Compose v2

Docker Compose v2 ships with Docker Desktop on macOS. On Linux, the `docker-compose-plugin` package installed above provides it. Verify with:

```bash
docker compose version
```

#### Git

**macOS (Homebrew):**

```bash
brew install git
```

**Linux (Ubuntu/Debian):**

```bash
sudo apt-get install -y git
```

### Optional Tools

These are not required but are useful for development and debugging.

#### Go 1.22 (for local microservice development outside Docker)

**macOS:**

```bash
brew install go
```

**Linux:**

```bash
wget https://go.dev/dl/go1.22.0.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
```

#### curl and jq (for testing endpoints and parsing JSON)

**macOS:**

```bash
# curl is pre-installed on macOS
brew install jq
```

**Linux:**

```bash
sudo apt-get install -y curl jq
```

---

## 2. Verifying Your Tools

Run each of the following commands and confirm the output matches what is shown.

```bash
docker --version
# Expected: Docker version 24.x.x or higher (e.g., Docker version 27.4.0, build bde2b89)

docker compose version
# Expected: Docker Compose version v2.x.x (e.g., Docker Compose version v2.32.4)

git --version
# Expected: git version 2.x.x (e.g., git version 2.43.0)
```

Verify Docker is running:

```bash
docker info | head -5
# You should see output starting with "Client:" and "Server:" sections.
# If you see "Cannot connect to the Docker daemon", Docker Desktop is not running.
```

Check Docker resource allocation:

```bash
docker info | grep -E "CPUs|Total Memory"
# Expected: CPUs: 4 (or more)
# Expected: Total Memory: 3.xxx GiB (or more -- should be at least 4 GiB)
```

Optional tools:

```bash
go version
# Expected: go1.22.x (e.g., go1.22.0 linux/amd64)

curl --version | head -1
# Expected: curl 8.x.x (or 7.x.x)

jq --version
# Expected: jq-1.7.x (e.g., jq-1.7.1)
```

---

## 3. Step-by-Step Local Setup

### Step 1: Clone the Repository

```bash
git clone https://github.com/<your-username>/sre-observability-platform.git
cd sre-observability-platform
```

### Step 2: Start All Services

Run Docker Compose to build and start the entire platform:

```bash
docker compose up -d
```

This single command does the following:

| Service | What It Does | Port |
|---------|-------------|------|
| **order-service** | Go microservice simulating an order management API (~2% error rate) | 8081 |
| **payment-service** | Go microservice simulating payment processing (~5% error rate, varied latency by payment type) | 8082 |
| **user-service** | Go microservice simulating user management and authentication (~0.1% error rate, in-memory cache) | 8083 |
| **load-generator** | Generates realistic traffic with diurnal patterns and burst simulation | 8090 |
| **prometheus** | Scrapes metrics from all services every 15 seconds, evaluates recording and alerting rules | 9090 |
| **grafana** | Visualizes metrics from Prometheus and logs from Loki via pre-provisioned dashboards | 3000 |
| **alertmanager** | Receives alerts from Prometheus and routes them by severity | 9093 |
| **loki** | Aggregates and indexes logs for querying through Grafana | 3100 |
| **promtail** | Ships logs from Docker containers to Loki | -- |
| **node-exporter** | Exposes host-level metrics (CPU, memory, disk, network) | 9100 |

The first run will take 2-5 minutes because Docker needs to:
1. Pull base images (Prometheus, Grafana, Loki, etc.)
2. Build Go microservice images using multi-stage Dockerfiles
3. Start all containers and wait for health checks to pass

### Step 3: Verify All Services Are Healthy

Wait about 60-90 seconds after `docker compose up -d`, then check the status:

```bash
docker compose ps
```

You should see output similar to this (all services showing "healthy" or "running"):

```
NAME              IMAGE                        STATUS                   PORTS
alertmanager      prom/alertmanager:v0.27.0     Up 45 seconds (healthy)  0.0.0.0:9093->9093/tcp
grafana           grafana/grafana:10.4.0        Up 44 seconds (healthy)  0.0.0.0:3000->3000/tcp
loki              grafana/loki:2.9.5            Up 45 seconds (healthy)  0.0.0.0:3100->3100/tcp
load-generator    sre-observ...-load-generator  Up 30 seconds            0.0.0.0:8090->8090/tcp
node-exporter     prom/node-exporter:v1.7.0     Up 45 seconds            0.0.0.0:9100->9100/tcp
order-service     sre-observ...-order-service   Up 45 seconds (healthy)  0.0.0.0:8081->8081/tcp
payment-service   sre-observ...-payment-service Up 45 seconds (healthy)  0.0.0.0:8082->8082/tcp
prometheus        prom/prometheus:v2.51.0       Up 45 seconds (healthy)  0.0.0.0:9090->9090/tcp
promtail          grafana/promtail:2.9.5        Up 44 seconds
user-service      sre-observ...-user-service    Up 45 seconds (healthy)  0.0.0.0:8083->8083/tcp
```

If any service is not yet healthy, wait another 30 seconds and check again. The load-generator starts last because it waits for all three microservices to pass their health checks first.

### Step 4: Open Grafana

Open your browser and navigate to:

```
http://localhost:3000
```

Log in with:
- **Username:** `admin`
- **Password:** `admin`

You will be prompted to change the password. You can either set a new password or click "Skip" to keep using the default.

### Step 5: Navigate to the Dashboards

After logging in to Grafana:

1. Click the hamburger menu (three horizontal lines) in the top-left corner
2. Click **Dashboards**
3. You will see a folder called **SRE** -- click on it
4. You will see three dashboards:
   - **SLO Overview** -- Error budgets, SLI compliance, burn rates
   - **Service Health** -- RED metrics (Rate, Errors, Duration) per service
   - **Infrastructure** -- Node-level resource utilization

Click on each dashboard. Within 2-3 minutes of the load generator running, you will start seeing data populate the panels. If panels show "No data", wait a bit longer -- Prometheus needs time to accumulate enough scrape data.

### Step 6: Explore Prometheus

Open Prometheus in your browser:

```
http://localhost:9090
```

Click on the "Graph" tab. Try these example queries in the expression input box:

**See all scrape targets and their status:**

```promql
up
```

This returns 1 (up) or 0 (down) for each target Prometheus scrapes. You should see your microservices and Prometheus itself all returning 1.

**See total request counts:**

```promql
http_requests_total
```

This shows the raw counter for HTTP requests across all services, broken down by method, path, and status code.

**See request rate (requests per second over the last 5 minutes):**

```promql
rate(http_requests_total[5m])
```

This converts the raw counter into a per-second rate, which is much more useful for understanding current throughput.

**See p99 latency (99th percentile response time):**

```promql
histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))
```

This calculates the response time that 99% of requests are faster than. Values above 0.5 (500ms) are worth investigating.

### Step 7: Check Alertmanager

Open Alertmanager in your browser:

```
http://localhost:9093
```

You will see the Alertmanager UI showing the current state of alerts. By default, you may see no active alerts -- this is normal. Alerts only fire when SLO thresholds are breached (for example, when error rates exceed the burn rate thresholds).

If you do see alerts, click on them to see the details including the service name, severity, and description.

### Step 8: Hit the Microservices Directly

Use curl to interact with the microservices and see their responses.

**List orders:**

```bash
curl -s http://localhost:8081/api/orders | jq .
```

Expected output:

```json
[
  {
    "id": "ord-001",
    "user_id": "usr-100",
    "items": ["item-a", "item-b"],
    "total": 99.99,
    "status": "completed",
    "created_at": "2024-01-01T..."
  },
  {
    "id": "ord-002",
    "user_id": "usr-101",
    "items": ["item-c"],
    "total": 49.5,
    "status": "processing",
    "created_at": "2024-01-01T..."
  }
]
```

**Create a new order (this calls user-service and payment-service internally):**

```bash
curl -s -X POST http://localhost:8081/api/orders | jq .
```

Expected output:

```json
{
  "id": "ord-000001",
  "user_id": "usr-342",
  "items": ["item-x", "item-y"],
  "total": 123.45,
  "status": "created",
  "created_at": "2024-01-01T..."
}
```

Note: This endpoint has a ~2% simulated error rate, so occasionally you will see a 500 or 502 response. This is intentional -- it drives the SLO dashboards.

**List payments:**

```bash
curl -s http://localhost:8082/api/payments | jq .
```

**List users:**

```bash
curl -s http://localhost:8083/api/users | jq .
```

**Authenticate a user:**

```bash
curl -s -X POST http://localhost:8083/api/users/auth | jq .
```

This simulates authentication with realistic outcomes: ~85% success, ~7% invalid credentials, ~5% account locked, ~3% rate limited.

**View raw Prometheus metrics from a service:**

```bash
curl -s http://localhost:8081/metrics | head -30
```

This shows the raw Prometheus exposition format that Prometheus scrapes. You will see metrics like `http_requests_total`, `http_request_duration_seconds_bucket`, `orders_created_total`, and Go runtime metrics.

### Step 9: Watch the Load Generator in Action

The load generator starts automatically and generates traffic against all three services with realistic patterns:

- **Diurnal pattern**: Traffic follows a cosine curve based on time of day (peak at noon, trough at 3 AM)
- **Burst simulation**: ~2% chance of a traffic burst at 5x normal rate for 30 seconds
- **Weighted endpoints**: Different endpoints are hit at different frequencies (e.g., GET is more common than POST)
- **Base rate**: 10 requests per second per service (configurable via `BASE_RPS` environment variable)

To confirm the load generator is running and see its metrics:

```bash
curl -s http://localhost:8090/metrics | grep loadgen_requests_sent_total
```

Open the Grafana dashboards and watch the graphs update in real-time as the load generator sends traffic.

### Step 10: View Service Logs

You can stream logs from any service using Docker Compose:

```bash
# Stream order-service logs
docker compose logs -f order-service

# Stream payment-service logs
docker compose logs -f payment-service

# Stream all service logs
docker compose logs -f order-service payment-service user-service

# Stream load-generator logs to see traffic patterns
docker compose logs -f load-generator

# See the last 50 lines of Prometheus logs
docker compose logs --tail=50 prometheus
```

Logs are structured JSON, so you can pipe them through jq for readability:

```bash
docker compose logs --tail=10 order-service --no-log-prefix 2>&1 | jq . 2>/dev/null
```

---

## 4. Exploring the Dashboards

### SLO Overview Dashboard

This is the most important dashboard for SRE work. It answers the question: "Are we meeting our reliability targets?"

**Error Budget Remaining (Gauge panels at the top):**
- Shows a percentage gauge for each service
- **Green (>50%)**: Healthy -- more than half the error budget remains
- **Yellow (20-50%)**: Caution -- the budget is being consumed
- **Red (<20%)**: Critical -- the budget is nearly exhausted or already over
- Formula: `1 - ((1 - actual_availability) / (1 - SLO_target))`
- For a 99.9% SLO target, you have a 0.1% error budget. If your actual error rate is 0.05%, you have consumed 50% of your budget.

**30-Day Rolling Availability SLI:**
- A time-series graph showing the percentage of non-5xx requests over a 30-day rolling window
- The target line is drawn at 99.9%
- If the line dips below the target, the SLO is being violated

**30-Day Rolling Latency SLI:**
- Shows the percentage of requests completing in under 500ms
- The target line is drawn at 99%
- Dips below the target indicate latency SLO violations

**Burn Rate Over Time:**
- Shows how fast the error budget is being consumed
- A burn rate of 1.0 means the budget is being consumed at exactly the sustainable rate
- A burn rate of 14.4 means the entire 30-day budget will be consumed in ~50 hours
- Spikes in burn rate correspond to periods of elevated errors

### Service Health Dashboard

This dashboard implements the RED method for each service. It answers: "What is happening right now?"

**Rate (Requests Per Second):**
- Shows the QPS (queries per second) for each service
- Useful for spotting traffic drops (possible outage upstream) or traffic spikes (possible DDoS or burst)

**Errors (Error Percentage):**
- Shows the percentage of 5xx responses per service
- A horizontal threshold line marks the SLO boundary
- You should see: order-service ~2%, payment-service ~5%, user-service ~0.1%

**Duration (Latency Distribution):**
- Shows p50, p90, p95, and p99 latency percentiles
- The p50 (median) is what most users experience
- The p99 shows worst-case performance (tail latency)
- The latency heatmap shows the distribution visually -- darker bands indicate more requests at that latency

### Infrastructure Dashboard

This dashboard implements the USE method for host resources. It answers: "Is the infrastructure under pressure?"

**CPU Utilization:**
- Shows the percentage of CPU time spent on non-idle work
- Sustained values above 80% warrant investigation

**Memory Utilization:**
- Shows the percentage of available memory in use
- Watch for steady upward trends that may indicate a memory leak

**Disk I/O and Network I/O:**
- Shows throughput for disk reads/writes and network in/out
- Useful for identifying storage bottlenecks (especially for Prometheus and Loki data)

**Node Resources Summary:**
- Aggregated view of all resources on the host
- In a Docker Compose setup, this represents your laptop or VM

---

## 5. Stopping and Cleaning Up

### Stop all services (preserves data volumes):

```bash
docker compose down
```

### Stop all services and remove data volumes (fresh start):

```bash
docker compose down -v
```

### Use the cleanup script (removes everything including built images):

```bash
./scripts/cleanup.sh
```

This script runs:
1. `docker-compose down -v --remove-orphans` -- stops containers, removes volumes and orphans
2. `docker-compose down --rmi local` -- removes locally built images

After cleanup, the next `docker compose up -d` will rebuild everything from scratch.

---

## 6. What to Explore Next

Now that you have the platform running, here are some things to try:

1. **Trigger an SLO breach**: Increase the error rate in one of the microservices (edit the error probability in the Go code, rebuild with `docker compose build order-service`, and restart with `docker compose up -d order-service`). Watch the SLO dashboard react.

2. **Read the Prometheus recording rules**: Open `monitoring/prometheus/rules/slo-rules.yml` to understand how multi-window multi-burn-rate alerting is implemented. The comments explain each rule.

3. **Explore PromQL**: Try writing your own queries in the Prometheus UI. Start with the metric names visible at `http://localhost:8081/metrics` and use `rate()`, `histogram_quantile()`, and `sum by()`.

4. **Read the Architecture doc**: See [ARCHITECTURE.md](ARCHITECTURE.md) for a deep technical explanation of how all the components fit together, including the SRE methodology behind the SLO/SLI design.

5. **Understand the alert routing**: Open `monitoring/alertmanager/alertmanager.yml` to see how alerts are routed to PagerDuty (critical), Slack (warning), and email (info) based on severity labels.

6. **Explore Loki logs in Grafana**: In Grafana, go to the Explore page (compass icon in the left sidebar), select the Loki datasource, and run a query like `{container="order-service"}` to see structured logs.

7. **Review the Terraform modules**: Look at `terraform/modules/monitoring/main.tf` to see how the cloud infrastructure (S3 for log storage, KMS for encryption, IAM roles for IRSA, SNS for alert routing) would be provisioned on AWS.

8. **Study the CI/CD pipelines**: Check `.github/workflows/ci.yaml` and `.github/workflows/cd.yaml` to see how the project handles linting, testing, building, security scanning, and deployment.

9. **Customize the load generator**: Change the `BASE_RPS` environment variable in `docker-compose.yml` to increase or decrease traffic. Set it to 50 for a high-load scenario and watch how the dashboards respond.

10. **Read the Troubleshooting guide**: If anything goes wrong, see [TROUBLESHOOTING.md](TROUBLESHOOTING.md) for solutions to common issues.
