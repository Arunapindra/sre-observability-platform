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

	ordersCreatedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "orders_created_total",
			Help: "Total number of orders created.",
		},
	)

	ordersInProgress = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "orders_in_progress",
			Help: "Number of orders currently being processed.",
		},
	)

	orderProcessingDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "order_processing_duration_seconds",
			Help:    "Duration of order processing in seconds.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.0, 5.0},
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

type Order struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Items     []string  `json:"items"`
	Total     float64   `json:"total"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Code    int    `json:"code"`
	TraceID string `json:"trace_id,omitempty"`
}

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

type Server struct {
	logger         *slog.Logger
	paymentBreaker *gobreaker.CircuitBreaker
	userBreaker    *gobreaker.CircuitBreaker
	paymentURL     string
	userURL        string
	httpClient     *http.Client
	ready          atomic.Bool
	orderCounter   atomic.Int64
}

func newServer(logger *slog.Logger) *Server {
	paymentURL := getEnv("PAYMENT_SERVICE_URL", "http://payment-service:8082")
	userURL := getEnv("USER_SERVICE_URL", "http://user-service:8083")

	s := &Server{
		logger:     logger,
		paymentURL: paymentURL,
		userURL:    userURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	cbSettings := func(name string) gobreaker.Settings {
		return gobreaker.Settings{
			Name:        name,
			MaxRequests: 3,
			Interval:    10 * time.Second,
			Timeout:     30 * time.Second,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				ratio := float64(counts.TotalFailures) / float64(counts.Requests)
				return counts.Requests >= 5 && ratio >= 0.5
			},
			OnStateChange: func(n string, from, to gobreaker.State) {
				logger.Warn("circuit breaker state change",
					"service", n, "from", from.String(), "to", to.String())
				v := 0.0
				switch to {
				case gobreaker.StateHalfOpen:
					v = 1
				case gobreaker.StateOpen:
					v = 2
				}
				circuitBreakerState.WithLabelValues(n).Set(v)
			},
		}
	}

	s.paymentBreaker = gobreaker.NewCircuitBreaker(cbSettings("payment-service"))
	s.userBreaker = gobreaker.NewCircuitBreaker(cbSettings("user-service"))
	return s
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	prometheus.MustRegister(
		httpRequestsTotal, httpRequestDuration,
		ordersCreatedTotal, ordersInProgress, orderProcessingDuration,
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

	r.Route("/api/orders", func(r chi.Router) {
		r.Get("/", srv.handleListOrders)
		r.Post("/", srv.handleCreateOrder)
		r.Get("/{orderID}", srv.handleGetOrder)
	})

	port := getEnv("PORT", "8081")
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
		logger.Info("order-service starting", "port", port)
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

func (s *Server) handleListOrders(w http.ResponseWriter, r *http.Request) {
	time.Sleep(simulateLatency(50, 20, 0.02))

	if rand.Float64() < 0.02 {
		s.logger.Warn("simulated error listing orders",
			"request_id", middleware.GetReqID(r.Context()))
		writeError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	orders := []Order{
		{ID: "ord-001", UserID: "usr-100", Items: []string{"item-a", "item-b"}, Total: 99.99, Status: "completed", CreatedAt: time.Now().Add(-24 * time.Hour)},
		{ID: "ord-002", UserID: "usr-101", Items: []string{"item-c"}, Total: 49.50, Status: "processing", CreatedAt: time.Now().Add(-1 * time.Hour)},
	}
	writeJSON(w, http.StatusOK, orders)
}

func (s *Server) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "orderID")
	time.Sleep(simulateLatency(30, 10, 0.01))

	if rand.Float64() < 0.02 {
		s.logger.Warn("simulated error getting order", "orderID", orderID)
		writeError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	order := Order{
		ID: orderID, UserID: "usr-100", Items: []string{"item-a", "item-b"},
		Total: 99.99, Status: "completed", CreatedAt: time.Now().Add(-2 * time.Hour),
	}
	writeJSON(w, http.StatusOK, order)
}

func (s *Server) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	ordersInProgress.Inc()
	defer ordersInProgress.Dec()

	start := time.Now()
	seq := s.orderCounter.Add(1)
	s.logger.Info("creating order", "seq", seq,
		"request_id", middleware.GetReqID(r.Context()))

	time.Sleep(simulateLatency(200, 80, 0.05))

	// Validate user via user-service.
	if err := s.callDownstream(r.Context(), s.userBreaker, s.userURL+"/api/users/validate", http.MethodGet, "user-service"); err != nil {
		s.logger.Error("user validation failed", "error", err)
		orderProcessingDuration.Observe(time.Since(start).Seconds())
		writeError(w, "user validation failed", http.StatusBadGateway)
		return
	}

	// Process payment via payment-service.
	if err := s.callDownstream(r.Context(), s.paymentBreaker, s.paymentURL+"/api/payments", http.MethodPost, "payment-service"); err != nil {
		s.logger.Error("payment failed", "error", err)
		orderProcessingDuration.Observe(time.Since(start).Seconds())
		writeError(w, "payment processing failed", http.StatusBadGateway)
		return
	}

	// Simulate occasional internal errors (~2%).
	if rand.Float64() < 0.02 {
		s.logger.Warn("simulated internal error during order creation")
		orderProcessingDuration.Observe(time.Since(start).Seconds())
		writeError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ordersCreatedTotal.Inc()
	orderProcessingDuration.Observe(time.Since(start).Seconds())

	order := Order{
		ID:        fmt.Sprintf("ord-%06d", seq),
		UserID:    fmt.Sprintf("usr-%03d", rand.Intn(500)+100),
		Items:     []string{"item-x", "item-y"},
		Total:     float64(rand.Intn(50000)) / 100.0,
		Status:    "created",
		CreatedAt: time.Now(),
	}
	s.logger.Info("order created", "id", order.ID, "total", order.Total,
		"duration_ms", time.Since(start).Milliseconds())
	writeJSON(w, http.StatusCreated, order)
}

// ---------------------------------------------------------------------------
// Downstream calls with circuit breaker
// ---------------------------------------------------------------------------

func (s *Server) callDownstream(ctx context.Context, cb *gobreaker.CircuitBreaker, url, method, label string) error {
	_, err := cb.Execute(func() (interface{}, error) {
		req, err := http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			downstreamRequestsTotal.WithLabelValues(label, "error").Inc()
			return nil, fmt.Errorf("creating request: %w", err)
		}
		resp, err := s.httpClient.Do(req)
		if err != nil {
			downstreamRequestsTotal.WithLabelValues(label, "error").Inc()
			return nil, fmt.Errorf("calling %s: %w", label, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 500 {
			downstreamRequestsTotal.WithLabelValues(label, "error").Inc()
			return nil, fmt.Errorf("%s returned %d", label, resp.StatusCode)
		}
		downstreamRequestsTotal.WithLabelValues(label, "success").Inc()
		return nil, nil
	})
	return err
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// simulateLatency returns a duration drawn from a normal distribution.
// baseMsec is the mean, jitterMsec is the standard deviation.
// slowProb controls how often an extra-slow response occurs (P99 tail).
func simulateLatency(baseMsec, jitterMsec, slowProb float64) time.Duration {
	delay := baseMsec + jitterMsec*rand.NormFloat64()
	if delay < 1 {
		delay = 1
	}
	// Occasionally inject a very slow response to simulate tail latency.
	if rand.Float64() < slowProb {
		delay += baseMsec * (3 + rand.Float64()*7) // 3x-10x slower
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
