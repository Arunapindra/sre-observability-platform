#!/usr/bin/env bash
###############################################################################
# setup-local.sh - Start the full SRE Observability Platform locally
###############################################################################
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'
BOLD='\033[1m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

log_info()    { echo -e "${BLUE}[INFO]${NC} $*"; }
log_success() { echo -e "${GREEN}[OK]${NC} $*"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error()   { echo -e "${RED}[ERROR]${NC} $*" >&2; }
log_header()  { echo -e "\n${BOLD}${CYAN}=== $* ===${NC}\n"; }

# ─── Check prerequisites ───────────────────────────────────────────
log_header "Checking Prerequisites"

for tool in docker docker-compose; do
    if command -v "$tool" &>/dev/null; then
        log_success "$tool is installed"
    else
        log_error "$tool is not installed. Please install it first."
        exit 1
    fi
done

if ! docker info &>/dev/null; then
    log_error "Docker daemon is not running. Please start Docker Desktop."
    exit 1
fi
log_success "Docker daemon is running"

# ─── Build and start services ──────────────────────────────────────
log_header "Starting SRE Observability Platform"

cd "$PROJECT_ROOT"

log_info "Building microservices..."
docker-compose build --parallel

log_info "Starting all services..."
docker-compose up -d

# ─── Wait for services to be healthy ──────────────────────────────
log_header "Waiting for Services"

SERVICES=("prometheus:9090/-/healthy" "grafana:3000/api/health" "alertmanager:9093/-/healthy" "loki:3100/ready")
MAX_WAIT=120
WAITED=0

for svc in "${SERVICES[@]}"; do
    NAME="${svc%%:*}"
    ENDPOINT="${svc#*:}"
    log_info "Waiting for ${NAME}..."

    while [ $WAITED -lt $MAX_WAIT ]; do
        if wget -qO- "http://localhost:${ENDPOINT}" &>/dev/null; then
            log_success "${NAME} is healthy"
            break
        fi
        sleep 2
        WAITED=$((WAITED + 2))
    done

    if [ $WAITED -ge $MAX_WAIT ]; then
        log_warn "${NAME} did not become healthy within ${MAX_WAIT}s"
    fi
done

# ─── Print access information ─────────────────────────────────────
log_header "Platform is Ready!"

echo -e "${BOLD}Access URLs:${NC}"
echo -e "  Grafana:        ${CYAN}http://localhost:3000${NC}  (admin/admin)"
echo -e "  Prometheus:     ${CYAN}http://localhost:9090${NC}"
echo -e "  Alertmanager:   ${CYAN}http://localhost:9093${NC}"
echo -e "  Loki:           ${CYAN}http://localhost:3100${NC}"
echo ""
echo -e "${BOLD}Microservices:${NC}"
echo -e "  Order Service:   ${CYAN}http://localhost:8081${NC}"
echo -e "  Payment Service: ${CYAN}http://localhost:8082${NC}"
echo -e "  User Service:    ${CYAN}http://localhost:8083${NC}"
echo -e "  Load Generator:  ${CYAN}http://localhost:8090${NC}"
echo ""
echo -e "${BOLD}Quick Start:${NC}"
echo -e "  1. Open Grafana at http://localhost:3000"
echo -e "  2. Login with admin/admin"
echo -e "  3. Navigate to Dashboards > SLO Overview"
echo -e "  4. Watch metrics populate as the load generator runs"
echo ""
echo -e "${BOLD}Useful Commands:${NC}"
echo -e "  docker-compose logs -f order-service    # Stream service logs"
echo -e "  docker-compose ps                       # Check service status"
echo -e "  ./scripts/cleanup.sh                    # Tear down everything"
echo ""

log_success "SRE Observability Platform is running!"
