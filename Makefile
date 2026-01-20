.PHONY: help build test lint up down logs clean prometheus-reload integration-test load-test

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build all microservice Docker images
	docker compose build order-service payment-service user-service load-generator

test: ## Run Go unit tests for all microservices
	cd microservices/order-service && go test -v -race -coverprofile=coverage.out ./...
	cd microservices/payment-service && go test -v -race -coverprofile=coverage.out ./...
	cd microservices/user-service && go test -v -race -coverprofile=coverage.out ./...

lint: ## Lint Go code and YAML files
	cd microservices/order-service && golangci-lint run ./...
	cd microservices/payment-service && golangci-lint run ./...
	cd microservices/user-service && golangci-lint run ./...
	yamllint monitoring/ kubernetes/

up: ## Start the full observability stack
	docker compose up -d
	@echo "Waiting for services to be healthy..."
	@sleep 10
	@docker compose ps
	@echo ""
	@echo "Grafana:      http://localhost:3000 (admin/admin)"
	@echo "Prometheus:   http://localhost:9090"
	@echo "Alertmanager: http://localhost:9093"

down: ## Stop all services
	docker compose down

logs: ## Tail logs for all services
	docker compose logs -f --tail=50

prometheus-reload: ## Hot-reload Prometheus configuration
	curl -X POST http://localhost:9090/-/reload
	@echo "Prometheus config reloaded"

grafana-reload: ## Restart Grafana to pick up dashboard changes
	docker compose restart grafana

clean: ## Remove all containers, volumes, and images
	docker compose down -v --rmi local
	docker system prune -f

integration-test: ## Run integration tests against running services
	@echo "Testing service health endpoints..."
	curl -sf http://localhost:8081/healthz || (echo "order-service FAILED" && exit 1)
	curl -sf http://localhost:8082/healthz || (echo "payment-service FAILED" && exit 1)
	curl -sf http://localhost:8083/healthz || (echo "user-service FAILED" && exit 1)
	@echo "Testing Prometheus targets..."
	curl -sf http://localhost:9090/api/v1/targets | jq '.data.activeTargets | length'
	@echo "All integration tests passed!"

load-test: ## Run load test and check SLO compliance
	./scripts/load-test.sh

validate-rules: ## Validate Prometheus rules with promtool
	docker run --rm -v $$(pwd)/monitoring/prometheus:/etc/prometheus prom/prometheus:v2.51.0 \
		promtool check rules /etc/prometheus/rules/*.yml
	docker run --rm -v $$(pwd)/monitoring/prometheus:/etc/prometheus prom/prometheus:v2.51.0 \
		promtool check config /etc/prometheus/prometheus.yml

test-rules: ## Run Prometheus rule unit tests
	docker run --rm -v $$(pwd)/monitoring/prometheus:/etc/prometheus prom/prometheus:v2.51.0 \
		promtool test rules /etc/prometheus/tests/*.yml
