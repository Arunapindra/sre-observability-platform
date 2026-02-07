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
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

	userRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "user_requests_total",
			Help: "Total user-related requests.",
		},
		[]string{"operation"},
	)

	userAuthAttemptsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "user_auth_attempts_total",
			Help: "Total authentication attempts.",
		},
		[]string{"result"},
	)

	activeSessions = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "active_sessions",
			Help: "Number of active user sessions.",
		},
	)

	cacheHitsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cache_hits_total",
			Help: "Total cache hits and misses.",
		},
		[]string{"result"},
	)

	cacheLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cache_operation_duration_seconds",
			Help:    "Duration of cache operations in seconds.",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05},
		},
		[]string{"operation"},
	)

	userDBQueryDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "user_db_query_duration_seconds",
			Help:    "Duration of database queries in seconds.",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5},
		},
	)
)

// ---------------------------------------------------------------------------
// Domain types
// ---------------------------------------------------------------------------

type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type ErrorResponse struct {
	Error string `json:"error"`
	Code  int    `json:"code"`
}

// ---------------------------------------------------------------------------
// Simple in-memory cache for simulation
// ---------------------------------------------------------------------------

type userCache struct {
	mu    sync.RWMutex
	store map[string]*User
}

func newUserCache() *userCache {
	return &userCache{store: make(map[string]*User)}
}

func (c *userCache) Get(id string) (*User, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	u, ok := c.store[id]
	return u, ok
}

func (c *userCache) Set(id string, u *User) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[id] = u
}

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

type Server struct {
	logger       *slog.Logger
	cache        *userCache
	ready        atomic.Bool
	sessionCount atomic.Int64
}

func newServer(logger *slog.Logger) *Server {
	s := &Server{
		logger: logger,
		cache:  newUserCache(),
	}

	// Pre-populate cache with some users.
	for i := 100; i < 150; i++ {
		id := fmt.Sprintf("usr-%03d", i)
		s.cache.Set(id, &User{
			ID:        id,
			Username:  fmt.Sprintf("user_%d", i),
			Email:     fmt.Sprintf("user%d@example.com", i),
			Status:    "active",
			CreatedAt: time.Now().Add(-time.Duration(rand.Intn(365*24)) * time.Hour),
		})
	}

	// Simulate session count fluctuations.
	go s.simulateSessionGauge()

	return s
}

func (s *Server) simulateSessionGauge() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	baseSessions := int64(150)
	for range ticker.C {
		// Simulate diurnal pattern in sessions.
		hour := time.Now().Hour()
		var multiplier float64
		if hour >= 9 && hour <= 17 {
			multiplier = 1.0 + rand.Float64()*0.5 // Business hours: higher
		} else if hour >= 18 && hour <= 22 {
			multiplier = 0.7 + rand.Float64()*0.3 // Evening: moderate
		} else {
			multiplier = 0.2 + rand.Float64()*0.2 // Night: low
		}
		sessions := int64(float64(baseSessions)*multiplier) + int64(rand.NormFloat64()*20)
		if sessions < 10 {
			sessions = 10
		}
		s.sessionCount.Store(sessions)
		activeSessions.Set(float64(sessions))
	}
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	prometheus.MustRegister(
		httpRequestsTotal, httpRequestDuration,
		userRequestsTotal, userAuthAttemptsTotal,
		activeSessions, cacheHitsTotal, cacheLatency,
		userDBQueryDuration,
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

	r.Route("/api/users", func(r chi.Router) {
		r.Get("/", srv.handleListUsers)
		r.Post("/", srv.handleCreateUser)
		r.Get("/validate", srv.handleValidateUser)
		r.Get("/{userID}", srv.handleGetUser)
		r.Post("/auth", srv.handleAuthenticate)
	})

	port := getEnv("PORT", "8083")
	httpServer := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		time.Sleep(1 * time.Second)
		srv.ready.Store(true)
		logger.Info("service is ready")
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info("user-service starting", "port", port)
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

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	userRequestsTotal.WithLabelValues("list").Inc()
	time.Sleep(simulateLatency(30, 10, 0.005))

	// Very low error rate (~0.1%).
	if rand.Float64() < 0.001 {
		s.logger.Warn("simulated error listing users")
		writeError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Simulate DB query.
	dbStart := time.Now()
	time.Sleep(simulateLatency(5, 2, 0.01))
	userDBQueryDuration.Observe(time.Since(dbStart).Seconds())

	users := []User{
		{ID: "usr-100", Username: "alice", Email: "alice@example.com", Status: "active", CreatedAt: time.Now().Add(-720 * time.Hour)},
		{ID: "usr-101", Username: "bob", Email: "bob@example.com", Status: "active", CreatedAt: time.Now().Add(-360 * time.Hour)},
		{ID: "usr-102", Username: "charlie", Email: "charlie@example.com", Status: "inactive", CreatedAt: time.Now().Add(-100 * time.Hour)},
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	userRequestsTotal.WithLabelValues("get").Inc()

	// Try cache first.
	cacheStart := time.Now()
	if user, ok := s.cache.Get(userID); ok {
		cacheLatency.WithLabelValues("get").Observe(time.Since(cacheStart).Seconds())
		cacheHitsTotal.WithLabelValues("hit").Inc()
		s.logger.Debug("cache hit", "userID", userID)
		writeJSON(w, http.StatusOK, user)
		return
	}
	cacheLatency.WithLabelValues("get").Observe(time.Since(cacheStart).Seconds())
	cacheHitsTotal.WithLabelValues("miss").Inc()

	// Simulate DB query on cache miss.
	dbStart := time.Now()
	time.Sleep(simulateLatency(15, 5, 0.01))
	userDBQueryDuration.Observe(time.Since(dbStart).Seconds())

	if rand.Float64() < 0.001 {
		writeError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	user := User{
		ID: userID, Username: "user_" + userID, Email: userID + "@example.com",
		Status: "active", CreatedAt: time.Now().Add(-200 * time.Hour),
	}

	// Populate cache.
	cacheSetStart := time.Now()
	s.cache.Set(userID, &user)
	cacheLatency.WithLabelValues("set").Observe(time.Since(cacheSetStart).Seconds())

	writeJSON(w, http.StatusOK, user)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	userRequestsTotal.WithLabelValues("create").Inc()
	time.Sleep(simulateLatency(50, 20, 0.01))

	if rand.Float64() < 0.001 {
		writeError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	dbStart := time.Now()
	time.Sleep(simulateLatency(20, 8, 0.01))
	userDBQueryDuration.Observe(time.Since(dbStart).Seconds())

	user := User{
		ID:        fmt.Sprintf("usr-%03d", rand.Intn(9000)+1000),
		Username:  fmt.Sprintf("newuser_%d", rand.Intn(10000)),
		Email:     fmt.Sprintf("newuser%d@example.com", rand.Intn(10000)),
		Status:    "active",
		CreatedAt: time.Now(),
	}

	s.cache.Set(user.ID, &user)
	s.logger.Info("user created", "id", user.ID, "username", user.Username)
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) handleValidateUser(w http.ResponseWriter, r *http.Request) {
	userRequestsTotal.WithLabelValues("validate").Inc()
	time.Sleep(simulateLatency(10, 5, 0.005))

	// Very reliable endpoint (~0.1% error rate).
	if rand.Float64() < 0.001 {
		writeError(w, "validation service error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"valid":   true,
		"user_id": fmt.Sprintf("usr-%03d", rand.Intn(500)+100),
	})
}

func (s *Server) handleAuthenticate(w http.ResponseWriter, r *http.Request) {
	userRequestsTotal.WithLabelValues("authenticate").Inc()
	time.Sleep(simulateLatency(80, 30, 0.02))

	// Simulate auth outcomes.
	roll := rand.Float64()
	switch {
	case roll < 0.85:
		// Successful auth.
		userAuthAttemptsTotal.WithLabelValues("success").Inc()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated": true,
			"token":         "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.simulated",
			"expires_in":    3600,
		})
	case roll < 0.92:
		// Invalid credentials.
		userAuthAttemptsTotal.WithLabelValues("invalid_credentials").Inc()
		s.logger.Info("authentication failed: invalid credentials")
		writeError(w, "invalid credentials", http.StatusUnauthorized)
	case roll < 0.97:
		// Account locked.
		userAuthAttemptsTotal.WithLabelValues("account_locked").Inc()
		s.logger.Warn("authentication failed: account locked")
		writeError(w, "account locked", http.StatusForbidden)
	default:
		// Rate limited.
		userAuthAttemptsTotal.WithLabelValues("rate_limited").Inc()
		s.logger.Warn("authentication rate limited")
		writeError(w, "too many requests", http.StatusTooManyRequests)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func simulateLatency(baseMsec, jitterMsec, slowProb float64) time.Duration {
	delay := baseMsec + jitterMsec*rand.NormFloat64()
	if delay < 0.5 {
		delay = 0.5
	}
	if rand.Float64() < slowProb {
		delay += baseMsec * (2 + rand.Float64()*5)
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
