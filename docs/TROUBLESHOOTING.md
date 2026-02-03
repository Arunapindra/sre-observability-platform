# Troubleshooting Guide

This guide covers common issues you may encounter when running the SRE Observability Platform locally, along with their causes and step-by-step solutions.

---

## Table of Contents

1. [Docker Compose Won't Start](#1-docker-compose-wont-start)
2. [Docker Resource Constraints](#2-docker-resource-constraints)
3. [Microservice Build Failures](#3-microservice-build-failures)
4. [Prometheus Shows "Down" Targets](#4-prometheus-shows-down-targets)
5. [Grafana Dashboards Show "No Data"](#5-grafana-dashboards-show-no-data)
6. [Alertmanager Shows No Alerts](#6-alertmanager-shows-no-alerts)
7. [Load Generator Not Starting](#7-load-generator-not-starting)
8. [Loki Not Becoming Healthy](#8-loki-not-becoming-healthy)
9. [Port Conflicts](#9-port-conflicts)
10. [Connection Refused Errors](#10-connection-refused-errors)
11. [Cannot Curl Microservice Endpoints](#11-cannot-curl-microservice-endpoints)
12. [Prometheus Query Returns Empty](#12-prometheus-query-returns-empty)
13. [How to Check Logs for Each Component](#13-how-to-check-logs-for-each-component)
14. [How to Rebuild a Single Service After Code Changes](#14-how-to-rebuild-a-single-service-after-code-changes)
15. [How to Reset Everything and Start Fresh](#15-how-to-reset-everything-and-start-fresh)
16. [Grafana Password Reset](#16-grafana-password-reset)

---

## 1. Docker Compose Won't Start

### Symptom

Running `docker compose up -d` fails with an error about ports already being in use:

```
Error response from daemon: driver failed programming external connectivity on endpoint grafana:
Bind for 0.0.0.0:3000 failed: port is already allocated
```

### Cause

Another process on your machine is already listening on one of the ports used by the platform. The ports used are:

| Port | Service |
|------|---------|
| 3000 | Grafana |
| 3100 | Loki |
| 8081 | Order Service |
| 8082 | Payment Service |
| 8083 | User Service |
| 8090 | Load Generator |
| 9090 | Prometheus |
| 9093 | Alertmanager |
| 9100 | Node Exporter |

### Solution

Find and stop the conflicting process:

```bash
# On macOS or Linux, find what is using a specific port (replace 3000 with the conflicting port):
lsof -i :3000

# Example output:
# COMMAND  PID  USER  FD  TYPE  DEVICE  SIZE/OFF  NODE  NAME
# grafana  1234 user  8u  IPv4  0x1234  0t0       TCP   *:3000 (LISTEN)

# Kill the process:
kill -9 <PID>
```

Alternatively, if you have a previous instance of this platform still running:

```bash
docker compose down
docker compose up -d
```

---

## 2. Docker Resource Constraints

### Symptom

Services start but crash or become unhealthy. You may see in `docker compose ps`:

```
prometheus    prom/prometheus:v2.51.0    Up 5 seconds (unhealthy)
```

Or containers exit with code 137 (killed by OOM):

```bash
docker compose ps
# STATUS: Exited (137)
```

### Cause

Docker Desktop does not have enough CPU or memory allocated. The full stack requires at least 4 CPUs and 4 GB of RAM.

### Solution

**macOS (Docker Desktop):**

1. Open Docker Desktop
2. Click the gear icon (Settings)
3. Go to **Resources** > **Advanced**
4. Set:
   - CPUs: 4 (minimum, 6 recommended)
   - Memory: 4 GB (minimum, 6 GB recommended)
   - Swap: 1 GB
   - Disk image size: 20 GB
5. Click **Apply & Restart**
6. Wait for Docker to restart, then run `docker compose up -d` again

**Linux:**

Docker on Linux uses the host's resources directly, so this is usually not an issue. If you are running in a VM, ensure the VM has at least 4 CPUs and 4 GB RAM.

Check current Docker resource usage:

```bash
docker stats --no-stream
```

---

## 3. Microservice Build Failures

### Symptom

`docker compose up -d` fails during the build phase:

```
#8 ERROR: process "/bin/sh -c go mod download" did not complete successfully: exit code: 1
```

### Cause

Go module download failed. This is usually caused by network issues, DNS resolution problems, or a proxy blocking outbound connections.

### Solution

**Retry the build (most common fix):**

```bash
docker compose build --no-cache order-service
docker compose up -d
```

**If behind a corporate proxy:**

```bash
docker compose build --build-arg HTTP_PROXY=http://proxy:8080 --build-arg HTTPS_PROXY=http://proxy:8080
```

**If Docker DNS is not resolving:**

Add DNS configuration to Docker Desktop:
1. Settings > Docker Engine
2. Add: `"dns": ["8.8.8.8", "8.8.4.4"]`
3. Apply & Restart

**If Dockerfile syntax errors after code changes:**

Verify the Dockerfile still exists and has not been accidentally modified:

```bash
cat microservices/order-service/Dockerfile
```

The Dockerfile expects `go.mod`, `go.sum`, and `.go` files in the service directory.

---

## 4. Prometheus Shows "Down" Targets

### Symptom

Opening http://localhost:9090/targets shows one or more targets with state "DOWN" (red).

### Cause

The target service has not started yet or has not passed its health check. Services have a startup delay:
- User service: 1 second readiness delay
- Order service: 2 second readiness delay
- Payment service: 2 second readiness delay
- Load generator: waits for all three services to be healthy, then waits 10 more seconds

Also, in the Docker Compose setup, Prometheus scrapes services by their container names on the `monitoring` Docker network. The `application-services` job in `prometheus.yml` references different hostnames and ports (for the Kubernetes deployment). For local development, the microservices expose their metrics on the `monitoring` network using their container names and actual ports.

### Solution

Wait 60-90 seconds after starting the stack, then refresh the targets page.

If a specific service stays down:

```bash
# Check if the service container is running
docker compose ps order-service

# Check the service logs for errors
docker compose logs order-service

# Restart just that service
docker compose restart order-service
```

Verify the service is responding:

```bash
curl http://localhost:8081/healthz
# Expected: {"status":"healthy"}

curl http://localhost:8081/metrics | head -5
# Expected: Lines starting with "# HELP" or "# TYPE"
```

---

## 5. Grafana Dashboards Show "No Data"

### Symptom

You open a Grafana dashboard and see "No data" in one or more panels.

### Cause

There are several possible causes:

1. **Prometheus has not scraped enough data yet.** Recording rules need at least 1-5 minutes of data to compute rates and ratios.
2. **The load generator has not started yet.** Without traffic, there are no metrics to display.
3. **The Prometheus datasource is not configured correctly.**
4. **The recording rule metric names in the dashboard do not match the actual metric names.**

### Solution

**Wait 2-3 minutes.** This is the most common fix. Prometheus needs time to accumulate scrape data, and recording rules need time to produce results.

**Verify the load generator is running:**

```bash
docker compose ps load-generator
# Should show "Up" status

docker compose logs --tail=5 load-generator
# Should show "starting traffic generation" messages
```

**Verify Prometheus is scraping data:**

1. Open http://localhost:9090
2. Enter the query `up` and click Execute
3. You should see entries for the microservices with value 1
4. Try `http_requests_total` -- if this returns data, Prometheus is scraping correctly

**Verify the Grafana datasource:**

1. In Grafana, go to Administration > Data Sources (or Settings > Data Sources)
2. Click on "Prometheus"
3. Click "Test" at the bottom
4. It should say "Data source is working"

If the datasource test fails, check that Prometheus is running:

```bash
curl http://localhost:9090/-/healthy
# Expected: Prometheus Server is Healthy.
```

---

## 6. Alertmanager Shows No Alerts

### Symptom

You open http://localhost:9093 and the alerts page is empty.

### Cause

This is normal. Alerts only fire when specific conditions are met (e.g., error rate exceeds thresholds for a sustained period). With the default simulated error rates (~2% for order-service, ~5% for payment-service), some alerts may or may not fire depending on the specific SLO thresholds configured.

### Solution

This is expected behavior. To see alerts, you can:

1. **Wait for the payment-service's ~5% error rate to trigger the `ElevatedErrorRate` alert** (threshold is 0.5% over 15 minutes). This should fire within 15-20 minutes of the load generator running.

2. **Check Prometheus for pending alerts:**
   Open http://localhost:9090/alerts to see which alert rules exist and their current state (inactive, pending, or firing).

3. **Verify Alertmanager is connected to Prometheus:**
   In Prometheus, go to Status > Runtime & Build Information. Under "Alertmanagers", you should see `http://alertmanager:9093/api/v2/alerts`.

---

## 7. Load Generator Not Starting

### Symptom

`docker compose ps load-generator` shows the container is not running or is in a restart loop.

### Cause

The load generator has a `depends_on` configuration that waits for all three microservices to pass their health checks:

```yaml
depends_on:
  order-service:
    condition: service_healthy
  payment-service:
    condition: service_healthy
  user-service:
    condition: service_healthy
```

If any service fails its health check, the load generator will not start.

### Solution

Check the health of the dependent services:

```bash
docker compose ps
```

Look for any service that is not "healthy". Then check its logs:

```bash
docker compose logs order-service
docker compose logs payment-service
docker compose logs user-service
```

Common issues:
- Service failed to build (see section 3)
- Port conflict (see section 9)
- Out of memory (see section 2)

Once all three services are healthy, the load generator will start automatically due to `restart: unless-stopped`.

---

## 8. Loki Not Becoming Healthy

### Symptom

`docker compose ps loki` shows the container as unhealthy or stuck in a restart loop.

### Cause

1. **Disk space:** Loki needs space for its WAL, chunks, and index data.
2. **Configuration syntax error:** If the loki-config.yml was modified and has invalid YAML.
3. **Permission issues:** The Loki data directory may have incorrect permissions.

### Solution

**Check Loki logs:**

```bash
docker compose logs loki
```

**If disk space is the issue:**

```bash
# Check available disk space
df -h

# Remove old Docker data to free space
docker system prune -a --volumes
```

**If configuration syntax error:**

```bash
# Validate the YAML file
docker run --rm -v $(pwd)/monitoring/loki/loki-config.yml:/etc/loki/loki-config.yml \
  grafana/loki:2.9.5 -config.file=/etc/loki/loki-config.yml -verify-config
```

**If permission issues:**

```bash
# Remove the Loki data volume and recreate
docker compose down -v
docker compose up -d
```

---

## 9. Port Conflicts

### Symptom

```
Bind for 0.0.0.0:XXXX failed: port is already allocated
```

### Solution

Find and kill the conflicting process for each port:

```bash
# Grafana (3000)
lsof -i :3000 | grep LISTEN
kill -9 <PID>

# Prometheus (9090)
lsof -i :9090 | grep LISTEN
kill -9 <PID>

# Alertmanager (9093)
lsof -i :9093 | grep LISTEN
kill -9 <PID>

# Loki (3100)
lsof -i :3100 | grep LISTEN
kill -9 <PID>

# Order Service (8081)
lsof -i :8081 | grep LISTEN
kill -9 <PID>

# Payment Service (8082)
lsof -i :8082 | grep LISTEN
kill -9 <PID>

# User Service (8083)
lsof -i :8083 | grep LISTEN
kill -9 <PID>

# Load Generator (8090)
lsof -i :8090 | grep LISTEN
kill -9 <PID>

# Node Exporter (9100)
lsof -i :9100 | grep LISTEN
kill -9 <PID>
```

If the conflicting process is a previous Docker Compose instance:

```bash
# Stop all containers from this project
docker compose down

# If that does not work, force-remove all project containers
docker ps -a --filter "name=order-service" --filter "name=payment-service" \
  --filter "name=user-service" --filter "name=load-generator" \
  --filter "name=prometheus" --filter "name=grafana" \
  --filter "name=alertmanager" --filter "name=loki" \
  --filter "name=promtail" --filter "name=node-exporter" -q | xargs docker rm -f
```

---

## 10. Connection Refused Errors

### Symptom

```bash
curl http://localhost:8081/api/orders
# curl: (7) Failed to connect to localhost port 8081: Connection refused
```

### Cause

1. The service container is not running yet.
2. The service is still starting up (hasn't passed the readiness delay).
3. Docker networking issue.

### Solution

**Check if the container is running:**

```bash
docker compose ps order-service
```

If it shows "Up" but not "healthy", the service is still starting. Wait 15-30 seconds.

**If the container is running but you still get connection refused:**

```bash
# Verify the port mapping is correct
docker port order-service
# Expected: 8081/tcp -> 0.0.0.0:8081

# Test from inside the Docker network
docker compose exec order-service wget -qO- http://localhost:8081/healthz
```

**If Docker networking is broken:**

```bash
# Restart Docker Desktop, then:
docker compose down
docker compose up -d
```

---

## 11. Cannot Curl Microservice Endpoints

### Symptom

`curl http://localhost:8081/api/orders` returns unexpected results or cannot connect, even though `docker compose ps` shows the service as healthy.

### Cause

This can happen when:
- Your machine's firewall is blocking the connection
- The Docker port mapping did not bind correctly
- You are using `http://127.0.0.1` instead of `http://localhost` (or vice versa) and one is blocked

### Solution

**Try both localhost and 127.0.0.1:**

```bash
curl http://localhost:8081/api/orders
curl http://127.0.0.1:8081/api/orders
```

**Verify the port is actually bound:**

```bash
# macOS
lsof -i :8081 | grep LISTEN

# Linux
ss -tlnp | grep 8081
```

**Test from inside the container:**

```bash
docker compose exec order-service wget -qO- http://localhost:8081/api/orders
```

If this works but `curl` from your host does not, the issue is in Docker's port forwarding or your host firewall.

---

## 12. Prometheus Query Returns Empty

### Symptom

You enter a PromQL query in the Prometheus UI (http://localhost:9090) and get an empty result.

### Cause

1. The metric name is wrong or has a typo.
2. The label selector does not match any time series.
3. The metric does not exist yet (not enough scrapes).
4. You are querying a recording rule metric that has not been evaluated yet.

### Solution

**Check the exact metric name:**

```bash
# See all metrics exposed by the order-service
curl -s http://localhost:8081/metrics | grep "^# HELP"
```

This lists all metric names. Common metric names:
- `http_requests_total` (not `http_request_total` -- note the plural)
- `http_request_duration_seconds_bucket` (not `http_request_duration_bucket`)

**Check what label values exist:**

In Prometheus, query:

```promql
http_requests_total
```

Look at the labels in the results. Common label mismatches:
- The label is `status` not `status_code` (depends on the service -- the microservices in this project use `status`)
- The label value is `200` not `"200"` (always use string matching)

**Check recording rules are producing data:**

```promql
slo:availability:ratio_rate5m
```

If this is empty, the recording rules may not have been evaluated yet. Check Status > Rules in the Prometheus UI to see rule evaluation state and any errors.

**Verify Prometheus is scraping the target:**

Go to http://localhost:9090/targets and confirm the target is in "UP" state.

---

## 13. How to Check Logs for Each Component

Use these commands to inspect logs for any component:

```bash
# Microservices (structured JSON logs)
docker compose logs -f order-service
docker compose logs -f payment-service
docker compose logs -f user-service
docker compose logs -f load-generator

# Observability stack
docker compose logs -f prometheus
docker compose logs -f grafana
docker compose logs -f alertmanager
docker compose logs -f loki
docker compose logs -f promtail
docker compose logs -f node-exporter

# Show only the last N lines
docker compose logs --tail=50 order-service

# Show logs from a specific time range
docker compose logs --since="5m" order-service

# Show logs from all services at once
docker compose logs -f

# Show logs without the container name prefix (useful for piping to jq)
docker compose logs --no-log-prefix order-service 2>&1 | jq . 2>/dev/null
```

**What to look for:**

| Component | Normal Log | Problem Log |
|-----------|-----------|-------------|
| order-service | `"msg":"order created"` | `"msg":"user validation failed"`, `"level":"ERROR"` |
| payment-service | `"msg":"payment processed"` | `"msg":"fraud check failed"`, `"msg":"payment declined"` |
| user-service | `"msg":"service is ready"` | `"level":"ERROR"` |
| load-generator | `"msg":"starting traffic generation"` | `"msg":"request failed"`, `"msg":"burst traffic started"` |
| prometheus | `msg="Server is ready to receive web requests."` | `err="opening storage failed"` |
| grafana | `msg="HTTP Server Listen"` | `msg="Failed to start"`, `lvl=eror` |
| alertmanager | `msg="Listening"` | `err="config file error"` |
| loki | `msg="Loki started"` | `msg="error"`, `level=error` |

---

## 14. How to Rebuild a Single Service After Code Changes

If you modify the Go source code of a microservice:

```bash
# Rebuild only the changed service
docker compose build order-service

# Restart only that service (other services stay running)
docker compose up -d order-service

# Or combine into one command
docker compose up -d --build order-service
```

If you modify a monitoring configuration file (e.g., `prometheus.yml`, `alertmanager.yml`):

```bash
# Prometheus supports hot-reloading (no restart needed)
curl -X POST http://localhost:9090/-/reload

# For Alertmanager, restart the container
docker compose restart alertmanager

# For Grafana dashboards, they auto-refresh every 30 seconds from the filesystem
# No restart needed if you only changed dashboard JSON files
```

If you modify `docker-compose.yml` itself:

```bash
# Recreate affected services
docker compose up -d
```

---

## 15. How to Reset Everything and Start Fresh

**Option 1: Use the cleanup script (recommended)**

```bash
./scripts/cleanup.sh
```

This runs `docker-compose down -v --remove-orphans` (stops containers, removes volumes and orphans) and `docker-compose down --rmi local` (removes locally built images).

**Option 2: Manual cleanup**

```bash
# Stop all containers and remove volumes
docker compose down -v

# Also remove built images
docker compose down --rmi local

# Start fresh
docker compose up -d
```

**Option 3: Nuclear option (removes all Docker data, not just this project)**

```bash
# WARNING: This removes ALL Docker containers, images, volumes, and networks
# on your machine, not just this project.
docker system prune -a --volumes -f
```

After cleanup, the next `docker compose up -d` will:
1. Pull base images fresh from Docker Hub
2. Rebuild all four microservice images from scratch
3. Create new empty data volumes for Prometheus, Grafana, Loki, and Alertmanager

---

## 16. Grafana Password Reset

### Symptom

You cannot log in to Grafana at http://localhost:3000 because you changed the password and forgot it, or the password is not the default `admin`.

### Solution

**Option 1: Reset via Grafana CLI inside the container:**

```bash
docker compose exec grafana grafana cli admin reset-admin-password admin
```

This resets the admin password back to `admin`. Restart Grafana for the change to take effect:

```bash
docker compose restart grafana
```

**Option 2: Remove the Grafana volume (loses all custom changes):**

```bash
docker compose down
docker volume rm sre-observability-platform_grafana-data
docker compose up -d
```

This removes the Grafana database, which resets the password to the default set in `docker-compose.yml` (`admin`/`admin`). Provisioned dashboards and datasources will be restored automatically from the filesystem.

**Option 3: Set the password via environment variable:**

The `docker-compose.yml` file sets the admin password via:

```yaml
environment:
  - GF_SECURITY_ADMIN_PASSWORD=admin
```

If you want a different password, change this value, then:

```bash
docker compose up -d grafana
```

Note: The environment variable only sets the password on first run. If the Grafana database already exists with a different password, you need to either use Option 1 or Option 2.
