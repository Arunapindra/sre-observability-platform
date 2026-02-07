package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sony/gobreaker"
)

// ---------------------------------------------------------------------------
// Prometheus metrics
// ---------------------------------------------------------------------------

var (
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests processed.",
		},
		[]string{"method", "path", "status"},
	)

	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Duration of HTTP requests in seconds.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0},
		},
		[]string{"method", "path"},
	)

	paymentTransactionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "payment_transactions_total",
			Help: "Total number of payment transactions.",
		},
		[]string{"status", "type"},
	)

	paymentAmountTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "payment_amount_total",
			Help: "Total payment amount processed in cents.",
		},
		[]string{"currency"},
	)

	paymentProcessingDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "payment_processing_duration_seconds",
			Help:    "Duration of payment processing in seconds.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.0, 5.0, 10.0},
		},
	)

	paymentsInFlight = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "payments_in_flight",
			Help: "Number of payments currently being processed.",
		},
	)

	downstreamRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "downstream_requests_total",
			Help: "Total requests to downstream services.",
		},
		[]string{"service", "status"},
	)

	circuitBreakerState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "circuit_breaker_state",
			Help: "Current state of circuit breakers (0=closed, 1=half-open, 2=open).",
		},
		[]string{"service"},
	)
)

// ---------------------------------------------------------------------------
// Domain types
// ---------------------------------------------------------------------------

type Payment struct {
	ID            string    `json:"id"`
	OrderID       string    `json:"order_id"`
	Amount        float64   `json:"amount"`
	Currency      string    `json:"currency"`
	Status        string    `json:"status"`
	Type          string    `json:"type"`
	ProcessedAt   time.Time `json:"processed_at"`
}

type ErrorResponse struct {
	Error string `json:"error"`
	Code  int    `json:"code"`
}

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

type Server struct {
	logger          *slog.Logger
	fraudBreaker    *gobreaker.CircuitBreaker
	httpClient      *http.Client
	ready           atomic.Bool
	paymentCounter  atomic.Int64
}

func newServer(logger *slog.Logger) *Server {
	s := &Server{
		logger:     logger,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	s.fraudBreaker = gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        "fraud-detection",
		MaxRequests: 3,
		Interval:    10 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			ratio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 5 && ratio >= 0.6
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			logger.Warn("circuit breaker state change",
				"service", name, "from", from.String(), "to", to.String())
			v := 0.0
			switch to {
			case gobreaker.StateHalfOpen:
				v = 1
			case gobreaker.StateOpen:
				v = 2
			}
			circuitBreakerState.WithLabelValues(name).Set(v)
		},
	})

	return s
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	prometheus.MustRegister(
		httpRequestsTotal, httpRequestDuration,
		paymentTransactionsTotal, paymentAmountTotal,
		paymentProcessingDuration, paymentsInFlight,
		downstreamRequestsTotal, circuitBreakerState,
	)

	srv := newServer(logger)

	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)
	r.Use(srv.metricsMiddleware)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", srv.handleHealthz)
	r.Get("/readyz", srv.handleReadyz)
	r.Handle("/metrics", promhttp.Handler())

	r.Route("/api/payments", func(r chi.Router) {
		r.Get("/", srv.handleListPayments)
		r.Post("/", srv.handleProcessPayment)
		r.Get("/{paymentID}", srv.handleGetPayment)
	})

	port := getEnv("PORT", "8082")
	httpServer := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		time.Sleep(2 * time.Second)
		srv.ready.Store(true)
		logger.Info("service is ready")
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info("payment-service starting", "port", port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-stop
	logger.Info("shutting down")
	srv.ready.Store(false)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("forced shutdown", "error", err)
	}
	logger.Info("server stopped")
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		duration := time.Since(start).Seconds()
		status := fmt.Sprintf("%d", ww.Status())
		path := chi.RouteContext(r.Context()).RoutePattern()
		if path == "" {
			path = r.URL.Path
		}
		httpRequestsTotal.WithLabelValues(r.Method, path, status).Inc()
		httpRequestDuration.WithLabelValues(r.Method, path).Observe(duration)
	})
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	if !s.ready.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not ready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleListPayments(w http.ResponseWriter, r *http.Request) {
	time.Sleep(simulateLatency(40, 15, 0.03))

	if rand.Float64() < 0.05 {
		s.logger.Warn("simulated error listing payments")
		writeError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	payments := []Payment{
		{ID: "pay-001", OrderID: "ord-001", Amount: 99.99, Currency: "USD", Status: "completed", Type: "credit_card", ProcessedAt: time.Now().Add(-24 * time.Hour)},
		{ID: "pay-002", OrderID: "ord-002", Amount: 49.50, Currency: "USD", Status: "pending", Type: "debit_card", ProcessedAt: time.Now().Add(-1 * time.Hour)},
	}
	writeJSON(w, http.StatusOK, payments)
}

func (s *Server) handleGetPayment(w http.ResponseWriter, r *http.Request) {
	paymentID := chi.URLParam(r, "paymentID")
	time.Sleep(simulateLatency(25, 10, 0.02))

	if rand.Float64() < 0.05 {
		s.logger.Warn("simulated error getting payment", "paymentID", paymentID)
		writeError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	payment := Payment{
		ID: paymentID, OrderID: "ord-001", Amount: 99.99, Currency: "USD",
		Status: "completed", Type: "credit_card", ProcessedAt: time.Now().Add(-2 * time.Hour),
	}
	writeJSON(w, http.StatusOK, payment)
}

func (s *Server) handleProcessPayment(w http.ResponseWriter, r *http.Request) {
	paymentsInFlight.Inc()
	defer paymentsInFlight.Dec()

	start := time.Now()
	seq := s.paymentCounter.Add(1)

	// Determine payment type randomly for realistic distribution.
	paymentTypes := []string{"credit_card", "debit_card", "bank_transfer", "digital_wallet"}
	pType := paymentTypes[rand.Intn(len(paymentTypes))]
	amount := float64(rand.Intn(100000)) / 100.0

	s.logger.Info("processing payment", "seq", seq, "type", pType, "amount", amount,
		"request_id", middleware.GetReqID(r.Context()))

	// Simulate payment gateway latency -- credit cards are faster, bank transfers slower.
	switch pType {
	case "credit_card":
		time.Sleep(simulateLatency(150, 50, 0.04))
	case "debit_card":
		time.Sleep(simulateLatency(180, 60, 0.04))
	case "bank_transfer":
		time.Sleep(simulateLatency(500, 200, 0.08))
	case "digital_wallet":
		time.Sleep(simulateLatency(100, 30, 0.03))
	}

	// Simulate fraud check via circuit breaker (internal call).
	fraudErr := s.runFraudCheck()

	// Simulate higher error rate (~5%) for interesting SLO data.
	if rand.Float64() < 0.05 || fraudErr != nil {
		status := "declined"
		if fraudErr != nil {
			status = "fraud_check_failed"
			s.logger.Error("fraud check failed", "error", fraudErr)
		} else if rand.Float64() < 0.3 {
			status = "gateway_error"
			s.logger.Warn("payment gateway error", "type", pType)
		} else {
			s.logger.Warn("payment declined", "type", pType, "amount", amount)
		}
		paymentTransactionsTotal.WithLabelValues(status, pType).Inc()
		paymentProcessingDuration.Observe(time.Since(start).Seconds())
		writeError(w, "payment "+status, http.StatusPaymentRequired)
		return
	}

	paymentTransactionsTotal.WithLabelValues("success", pType).Inc()
	paymentAmountTotal.WithLabelValues("USD").Add(amount)
	paymentProcessingDuration.Observe(time.Since(start).Seconds())

	payment := Payment{
		ID:          fmt.Sprintf("pay-%06d", seq),
		OrderID:     fmt.Sprintf("ord-%06d", seq),
		Amount:      amount,
		Currency:    "USD",
		Status:      "completed",
		Type:        pType,
		ProcessedAt: time.Now(),
	}

	s.logger.Info("payment processed", "id", payment.ID, "amount", amount,
		"type", pType, "duration_ms", time.Since(start).Milliseconds())
	writeJSON(w, http.StatusCreated, payment)
}

// ---------------------------------------------------------------------------
// Internal services
// ---------------------------------------------------------------------------

func (s *Server) runFraudCheck() error {
	_, err := s.fraudBreaker.Execute(func() (interface{}, error) {
		// Simulate an internal fraud detection service call.
		time.Sleep(simulateLatency(20, 10, 0.02))

		// Simulate occasional fraud service failures (~3%).
		if rand.Float64() < 0.03 {
			downstreamRequestsTotal.WithLabelValues("fraud-detection", "error").Inc()
			return nil, fmt.Errorf("fraud detection service timeout")
		}
		downstreamRequestsTotal.WithLabelValues("fraud-detection", "success").Inc()
		return nil, nil
	})
	return err
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func simulateLatency(baseMsec, jitterMsec, slowProb float64) time.Duration {
	delay := baseMsec + jitterMsec*rand.NormFloat64()
	if delay < 1 {
		delay = 1
	}
	if rand.Float64() < slowProb {
		delay += baseMsec * (3 + rand.Float64()*7)
	}
	return time.Duration(delay) * time.Millisecond
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, msg string, code int) {
	writeJSON(w, code, ErrorResponse{Error: msg, Code: code})
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}
