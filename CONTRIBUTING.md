# Contributing to SRE Observability Platform

Thank you for your interest in contributing! This document provides guidelines for contributing to this project.

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/<your-username>/sre-observability-platform.git`
3. Create a feature branch: `git checkout -b feature/your-feature-name`
4. Set up the local environment: `make up`
5. Make your changes
6. Run tests: `make test`
7. Commit and push
8. Open a Pull Request

## Development Setup

See [docs/GETTING-STARTED.md](docs/GETTING-STARTED.md) for detailed setup instructions.

**Quick start:**
```bash
docker compose up -d
make integration-test
```

## Code Standards

### Go Microservices
- Run `gofmt` and `go vet` before committing
- Add unit tests for new endpoints
- All services must expose `/healthz`, `/readyz`, and `/metrics` endpoints
- Use structured logging (JSON format)
- Prometheus metrics must follow naming conventions: `http_requests_total`, `http_request_duration_seconds`

### Prometheus Rules
- Recording rules go in `monitoring/prometheus/rules/`
- Alert rules go in `monitoring/prometheus/alerts/`
- Validate with: `make validate-rules`
- Add unit tests in `monitoring/prometheus/tests/`

### Grafana Dashboards
- Export as JSON and place in `monitoring/grafana/dashboards/`
- Use variables (`$service`, `$namespace`) for reusability
- Include a description for every panel

### Terraform
- Run `terraform fmt` before committing
- Add Terratest tests for new modules
- Use consistent variable naming

## Commit Messages

Use conventional commit format:
```
feat: add new dashboard for network metrics
fix: correct SLO calculation for payment-service
docs: update troubleshooting guide
refactor: simplify circuit breaker logic
test: add unit tests for user-service auth endpoint
```

## SRE Runbook Template

When adding new alerts, include a runbook entry:

```markdown
### Alert: YourAlertName

**Severity:** critical | warning | info
**Service:** affected-service
**SLO Impact:** Does this affect an SLO? Which one?

**Symptoms:**
- What the user/operator sees

**Possible Causes:**
1. Most likely cause
2. Second most likely cause

**Investigation Steps:**
1. Check X: `kubectl logs ...`
2. Check Y: `curl ...`

**Resolution:**
1. If cause 1: do this
2. If cause 2: do that

**Escalation:**
- Page the on-call SRE if not resolved in 15 minutes
```

## Pull Request Process

1. Update documentation if you changed behavior
2. Add/update tests for your changes
3. Ensure `make test` and `make validate-rules` pass
4. Request review from at least one maintainer
5. Squash commits before merging

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
