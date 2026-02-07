#!/usr/bin/env bash
###############################################################################
# cleanup.sh - Tear down the SRE Observability Platform
###############################################################################
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${YELLOW}Stopping all services...${NC}"
cd "$PROJECT_ROOT"
docker-compose down -v --remove-orphans

echo -e "${YELLOW}Removing built images...${NC}"
docker-compose down --rmi local 2>/dev/null || true

echo -e "${GREEN}Cleanup complete!${NC}"
