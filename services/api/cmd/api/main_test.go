package main

import (
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/go-chi/chi/v5"
)

func TestLiveHealthEndpoint(t *testing.T) {
    router := chi.NewRouter()
    router.Get("/health/live", func(w http.ResponseWriter, r *http.Request) {
        writeJSON(w, http.StatusOK, map[string]string{"status":"ok"})
    })

    request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
    response := httptest.NewRecorder()
    router.ServeHTTP(response, request)

    if response.Code != http.StatusOK {
        t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
    }
    if got := response.Header().Get("Content-Type"); got != "application/json" {
        t.Fatalf("expected application/json, got %q", got)
    }
}
