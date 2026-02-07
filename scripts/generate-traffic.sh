#!/usr/bin/env bash
###############################################################################
# generate-traffic.sh - Start the load generator with configurable parameters
###############################################################################
set -euo pipefail

BASE_RPS="${1:-10}"

echo "Starting load generator with ${BASE_RPS} base RPS..."
echo "Press Ctrl+C to stop"
echo ""

docker-compose up -d load-generator

echo "Load generator is running. Monitor at:"
echo "  Metrics: http://localhost:8090/metrics"
echo "  Grafana: http://localhost:3000"
