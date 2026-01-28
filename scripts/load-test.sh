#!/bin/bash
set -euo pipefail

# SLO-aware load testing script
# Sends controlled traffic and validates SLO compliance after the test

DURATION=${1:-60}
CONCURRENCY=${2:-10}
ORDER_URL="http://localhost:8081"
PAYMENT_URL="http://localhost:8082"
USER_URL="http://localhost:8083"
PROMETHEUS_URL="http://localhost:9090"

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo "============================================"
echo "  SRE Observability Platform - Load Test"
echo "============================================"
echo "Duration:    ${DURATION}s"
echo "Concurrency: ${CONCURRENCY}"
echo ""

# Check services are up
echo "Checking service health..."
for svc in "$ORDER_URL/healthz" "$PAYMENT_URL/healthz" "$USER_URL/healthz"; do
    if ! curl -sf "$svc" > /dev/null 2>&1; then
        echo -e "${RED}FAIL: $svc is not healthy${NC}"
        echo "Run 'make up' first to start services."
        exit 1
    fi
done
echo -e "${GREEN}All services healthy${NC}"
echo ""

# Record baseline metrics
echo "Recording baseline metrics from Prometheus..."
BASELINE_ERRORS=$(curl -sf "$PROMETHEUS_URL/api/v1/query?query=sum(http_requests_total{code=~\"5..\"})%20or%20vector(0)" | jq -r '.data.result[0].value[1] // "0"')
BASELINE_TOTAL=$(curl -sf "$PROMETHEUS_URL/api/v1/query?query=sum(http_requests_total)%20or%20vector(0)" | jq -r '.data.result[0].value[1] // "0"')

# Run load test
echo "Starting load test..."
echo ""

TOTAL_REQUESTS=0
TOTAL_ERRORS=0

for i in $(seq 1 "$DURATION"); do
    for j in $(seq 1 "$CONCURRENCY"); do
        (
            # Randomly hit different endpoints
            RAND=$((RANDOM % 6))
            case $RAND in
                0|1) curl -sf "$ORDER_URL/api/orders" > /dev/null 2>&1 || true ;;
                2) curl -sf -X POST "$ORDER_URL/api/orders" > /dev/null 2>&1 || true ;;
                3) curl -sf "$PAYMENT_URL/api/payments" > /dev/null 2>&1 || true ;;
                4) curl -sf "$USER_URL/api/users" > /dev/null 2>&1 || true ;;
                5) curl -sf "$USER_URL/api/users/validate" > /dev/null 2>&1 || true ;;
            esac
        ) &
    done
    TOTAL_REQUESTS=$((TOTAL_REQUESTS + CONCURRENCY))

    # Progress bar
    PCT=$((i * 100 / DURATION))
    printf "\r  Progress: [%-50s] %d%% (%d requests)" "$(printf '#%.0s' $(seq 1 $((PCT / 2))))" "$PCT" "$TOTAL_REQUESTS"

    sleep 1
done

echo ""
echo ""
echo "Load test complete. Sent $TOTAL_REQUESTS requests."
echo ""

# Wait for Prometheus to scrape
echo "Waiting 15s for Prometheus to scrape latest metrics..."
sleep 15

# Check SLO compliance
echo ""
echo "============================================"
echo "  SLO Compliance Check"
echo "============================================"

# Availability SLO: 99.9%
CURRENT_ERRORS=$(curl -sf "$PROMETHEUS_URL/api/v1/query?query=sum(http_requests_total{code=~\"5..\"})%20or%20vector(0)" | jq -r '.data.result[0].value[1] // "0"')
CURRENT_TOTAL=$(curl -sf "$PROMETHEUS_URL/api/v1/query?query=sum(http_requests_total)%20or%20vector(0)" | jq -r '.data.result[0].value[1] // "0"')

TEST_ERRORS=$(echo "$CURRENT_ERRORS - $BASELINE_ERRORS" | bc)
TEST_TOTAL=$(echo "$CURRENT_TOTAL - $BASELINE_TOTAL" | bc)

if [ "$TEST_TOTAL" -gt 0 ]; then
    AVAILABILITY=$(echo "scale=4; 1 - ($TEST_ERRORS / $TEST_TOTAL)" | bc)
    echo "  Availability: ${AVAILABILITY} (SLO: 0.999)"

    if (( $(echo "$AVAILABILITY >= 0.999" | bc -l) )); then
        echo -e "  Status: ${GREEN}PASS${NC}"
    else
        echo -e "  Status: ${RED}FAIL${NC}"
    fi
else
    echo -e "  ${YELLOW}No requests recorded in Prometheus yet${NC}"
fi

# Latency SLO: p99 < 500ms
P99=$(curl -sf "$PROMETHEUS_URL/api/v1/query?query=histogram_quantile(0.99,rate(http_request_duration_seconds_bucket[5m]))" | jq -r '.data.result[0].value[1] // "N/A"')
echo "  p99 Latency: ${P99}s (SLO: < 0.5s)"

echo ""
echo "Check Grafana dashboards at http://localhost:3000 for detailed analysis."
