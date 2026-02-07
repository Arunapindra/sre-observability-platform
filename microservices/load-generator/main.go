package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ---------------------------------------------------------------------------
// Prometheus metrics for the load generator itself
// ---------------------------------------------------------------------------

var (
	requestsSentTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "loadgen_requests_sent_total",
			Help: "Total requests sent by the load generator.",
		},
		[]string{"service", "method", "path"},
	)

	responsesReceivedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "loadgen_responses_received_total",
			Help: "Total responses received by the load generator.",
		},
		[]string{"service", "status_code"},
	)

	requestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "loadgen_request_duration_seconds",
			Help:    "Duration of load generator requests in seconds.",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0},
		},
		[]string{"service"},
	)

	requestErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "loadgen_request_errors_total",
			Help: "Total request errors from the load generator.",
		},
		[]string{"service", "error_type"},
	)

	currentRPS = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "loadgen_current_rps",
			Help: "Current target requests per second.",
		},
		[]string{"service"},
	)
)

// ---------------------------------------------------------------------------
// Target service definition
// ---------------------------------------------------------------------------

type endpoint struct {
	Method string
	Path   string
	Weight float64 // relative probability of being chosen
}

type targetService struct {
	Name      string
	BaseURL   string
	Endpoints []endpoint
}

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

type config struct {
	OrderServiceURL   string
	PaymentServiceURL string
	UserServiceURL    string
	BaseRPS           float64   // base requests per second per service
	BurstMultiplier   float64   // how much to multiply during bursts
	BurstProbability  float64   // probability of a burst each cycle
	BurstDuration     time.Duration
	MetricsPort       string
}

func loadConfig() config {
	baseRPS, _ := strconv.ParseFloat(getEnv("BASE_RPS", "5"), 64)
	burstMult, _ := strconv.ParseFloat(getEnv("BURST_MULTIPLIER", "5"), 64)
	burstProb, _ := strconv.ParseFloat(getEnv("BURST_PROBABILITY", "0.02"), 64)
	burstDurSec, _ := strconv.Atoi(getEnv("BURST_DURATION_SEC", "30"))

	return config{
		OrderServiceURL:   getEnv("ORDER_SERVICE_URL", "http://order-service:8081"),
		PaymentServiceURL: getEnv("PAYMENT_SERVICE_URL", "http://payment-service:8082"),
		UserServiceURL:    getEnv("USER_SERVICE_URL", "http://user-service:8083"),
		BaseRPS:           baseRPS,
		BurstMultiplier:   burstMult,
		BurstProbability:  burstProb,
		BurstDuration:     time.Duration(burstDurSec) * time.Second,
		MetricsPort:       getEnv("METRICS_PORT", "8090"),
	}
}

// ---------------------------------------------------------------------------
// Load generator
// ---------------------------------------------------------------------------

type loadGenerator struct {
	logger   *slog.Logger
	cfg      config
	client   *http.Client
	targets  []targetService
}

func newLoadGenerator(logger *slog.Logger, cfg config) *loadGenerator {
	targets := []targetService{
		{
			Name:    "order-service",
			BaseURL: cfg.OrderServiceURL,
			Endpoints: []endpoint{
				{Method: "GET", Path: "/api/orders", Weight: 5},
				{Method: "POST", Path: "/api/orders", Weight: 3},
				{Method: "GET", Path: "/api/orders/ord-001", Weight: 2},
				{Method: "GET", Path: "/healthz", Weight: 1},
			},
		},
		{
			Name:    "payment-service",
			BaseURL: cfg.PaymentServiceURL,
			Endpoints: []endpoint{
				{Method: "GET", Path: "/api/payments", Weight: 4},
				{Method: "POST", Path: "/api/payments", Weight: 5},
				{Method: "GET", Path: "/api/payments/pay-001", Weight: 2},
				{Method: "GET", Path: "/healthz", Weight: 1},
			},
		},
		{
			Name:    "user-service",
			BaseURL: cfg.UserServiceURL,
			Endpoints: []endpoint{
				{Method: "GET", Path: "/api/users", Weight: 3},
				{Method: "GET", Path: "/api/users/usr-100", Weight: 4},
				{Method: "POST", Path: "/api/users/auth", Weight: 3},
				{Method: "GET", Path: "/api/users/validate", Weight: 2},
				{Method: "POST", Path: "/api/users", Weight: 1},
				{Method: "GET", Path: "/healthz", Weight: 1},
			},
		},
	}

	return &loadGenerator{
		logger:  logger,
		cfg:     cfg,
		client:  &http.Client{Timeout: 10 * time.Second},
		targets: targets,
	}
}

// diurnalMultiplier returns a traffic multiplier based on time of day,
// simulating realistic business-hours traffic patterns.
func diurnalMultiplier() float64 {
	hour := time.Now().Hour()
	// Model a smooth curve: peak at 10-14, trough at 2-5 AM.
	// Using a shifted cosine for a realistic shape.
	// Peak ~ 12:00, trough ~ 03:00
	radians := float64(hour-12) * math.Pi / 12.0
	base := 0.5 + 0.5*math.Cos(radians) // ranges 0.0 to 1.0
	// Scale so minimum traffic is 20% and max is 100%.
	return 0.2 + 0.8*base
}

// selectEndpoint picks a weighted-random endpoint.
func selectEndpoint(endpoints []endpoint) endpoint {
	totalWeight := 0.0
	for _, ep := range endpoints {
		totalWeight += ep.Weight
	}
	r := rand.Float64() * totalWeight
	cumulative := 0.0
	for _, ep := range endpoints {
		cumulative += ep.Weight
		if r <= cumulative {
			return ep
		}
	}
	return endpoints[len(endpoints)-1]
}

func (lg *loadGenerator) run(ctx context.Context) {
	var wg sync.WaitGroup

	for _, target := range lg.targets {
		wg.Add(1)
		go func(t targetService) {
			defer wg.Done()
			lg.generateTraffic(ctx, t)
		}(target)
	}

	wg.Wait()
}

func (lg *loadGenerator) generateTraffic(ctx context.Context, target targetService) {
	lg.logger.Info("starting traffic generation", "service", target.Name, "base_rps", lg.cfg.BaseRPS)

	inBurst := false
	burstEnd := time.Time{}

	for {
		select {
		case <-ctx.Done():
			lg.logger.Info("stopping traffic generation", "service", target.Name)
			return
		default:
		}

		// Calculate current RPS.
		rps := lg.cfg.BaseRPS * diurnalMultiplier()

		// Check for burst.
		now := time.Now()
		if !inBurst && rand.Float64() < lg.cfg.BurstProbability {
			inBurst = true
			burstEnd = now.Add(lg.cfg.BurstDuration)
			lg.logger.Warn("burst traffic started",
				"service", target.Name,
				"multiplier", lg.cfg.BurstMultiplier,
				"duration", lg.cfg.BurstDuration)
		}
		if inBurst {
			if now.After(burstEnd) {
				inBurst = false
				lg.logger.Info("burst traffic ended", "service", target.Name)
			} else {
				rps *= lg.cfg.BurstMultiplier
			}
		}

		currentRPS.WithLabelValues(target.Name).Set(rps)

		// Add some jitter to the interval.
		interval := time.Duration(float64(time.Second) / rps)
		jitter := time.Duration(float64(interval) * 0.3 * rand.NormFloat64())
		sleepDuration := interval + jitter
		if sleepDuration < time.Millisecond {
			sleepDuration = time.Millisecond
		}

		time.Sleep(sleepDuration)

		// Send a request.
		ep := selectEndpoint(target.Endpoints)
		go lg.sendRequest(target, ep)
	}
}

func (lg *loadGenerator) sendRequest(target targetService, ep endpoint) {
	url := target.BaseURL + ep.Path
	requestsSentTotal.WithLabelValues(target.Name, ep.Method, ep.Path).Inc()

	start := time.Now()

	var body io.Reader
	if ep.Method == "POST" {
		// Send a minimal JSON body for POST requests.
		body = strings.NewReader(`{"source":"load-generator"}`)
	}

	req, err := http.NewRequest(ep.Method, url, body)
	if err != nil {
		requestErrors.WithLabelValues(target.Name, "request_creation").Inc()
		lg.logger.Error("failed to create request", "error", err, "service", target.Name)
		return
	}
	if ep.Method == "POST" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "sre-load-generator/1.0")
	req.Header.Set("X-Request-Source", "load-generator")

	resp, err := lg.client.Do(req)
	duration := time.Since(start)
	requestDuration.WithLabelValues(target.Name).Observe(duration.Seconds())

	if err != nil {
		requestErrors.WithLabelValues(target.Name, "connection").Inc()
		// Only log connection errors occasionally to avoid spam.
		if rand.Float64() < 0.01 {
			lg.logger.Error("request failed", "error", err, "service", target.Name, "path", ep.Path)
		}
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) // drain body to reuse connection

	statusCode := fmt.Sprintf("%d", resp.StatusCode)
	responsesReceivedTotal.WithLabelValues(target.Name, statusCode).Inc()

	if resp.StatusCode >= 500 {
		if rand.Float64() < 0.1 {
			lg.logger.Warn("server error response",
				"service", target.Name, "path", ep.Path,
				"status", resp.StatusCode, "duration_ms", duration.Milliseconds())
		}
	}
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	prometheus.MustRegister(
		requestsSentTotal, responsesReceivedTotal,
		requestDuration, requestErrors, currentRPS,
	)

	cfg := loadConfig()
	lg := newLoadGenerator(logger, cfg)

	// Expose load generator's own metrics.
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	metricsServer := &http.Server{
		Addr:    ":" + cfg.MetricsPort,
		Handler: mux,
	}

	go func() {
		logger.Info("load-generator metrics server starting", "port", cfg.MetricsPort)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("metrics server failed", "error", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-stop
		logger.Info("shutting down load generator")
		cancel()
	}()

	logger.Info("load-generator starting",
		"order_service", cfg.OrderServiceURL,
		"payment_service", cfg.PaymentServiceURL,
		"user_service", cfg.UserServiceURL,
		"base_rps", cfg.BaseRPS,
		"burst_multiplier", cfg.BurstMultiplier,
		"burst_probability", cfg.BurstProbability,
	)

	// Wait a few seconds for services to be ready before generating load.
	logger.Info("waiting for services to start...")
	time.Sleep(10 * time.Second)

	lg.run(ctx)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	metricsServer.Shutdown(shutdownCtx)

	logger.Info("load-generator stopped")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}
