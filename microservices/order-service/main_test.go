package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthzEndpoint(t *testing.T) {
	req, err := http.NewRequest("GET", "/healthz", nil)
	if err \!= nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(healthzHandler)
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status \!= http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}
}

func TestReadyzEndpoint(t *testing.T) {
	req, err := http.NewRequest("GET", "/readyz", nil)
	if err \!= nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(readyzHandler)
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status \!= http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	req, err := http.NewRequest("GET", "/metrics", nil)
	if err \!= nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(rr, req)
	if status := rr.Code; status \!= http.StatusOK {
		t.Errorf("metrics endpoint returned wrong status code: got %v want %v", status, http.StatusOK)
	}
}
