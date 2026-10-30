package main

import (
    "context"
    "encoding/json"
    "errors"
    "log"
    "net/http"
    "os"
    "time"

    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"
    "github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
    Port        string
    DatabaseURL string
    AppEnv      string
}

func loadConfig() Config {
    port := os.Getenv("APP_PORT")
    if port == "" { port = "8080" }
    return Config{
        Port: port,
        DatabaseURL: os.Getenv("DATABASE_URL"),
        AppEnv: os.Getenv("APP_ENV"),
    }
}

func main() {
    cfg := loadConfig()
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    var db *pgxpool.Pool
    if cfg.DatabaseURL != "" {
        var err error
        db, err = pgxpool.New(ctx, cfg.DatabaseURL)
        if err != nil { log.Fatalf("database configuration: %v", err) }
        if err := db.Ping(ctx); err != nil { log.Fatalf("database ping: %v", err) }
        defer db.Close()
    }

    router := chi.NewRouter()
    router.Use(middleware.RequestID)
    router.Use(middleware.RealIP)
    router.Use(middleware.Recoverer)
    router.Use(middleware.Timeout(15 * time.Second))

    router.Get("/health/live", func(w http.ResponseWriter, r *http.Request) {
        writeJSON(w, http.StatusOK, map[string]string{"status":"ok"})
    })
    router.Get("/health/ready", func(w http.ResponseWriter, r *http.Request) {
        if db != nil {
            pingCtx, stop := context.WithTimeout(r.Context(), 2*time.Second)
            defer stop()
            if err := db.Ping(pingCtx); err != nil {
                writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status":"not_ready"})
                return
            }
        }
        writeJSON(w, http.StatusOK, map[string]string{"status":"ready"})
    })

    server := &http.Server{
        Addr: ":" + cfg.Port,
        Handler: router,
        ReadHeaderTimeout: 5*time.Second,
        ReadTimeout: 10*time.Second,
        WriteTimeout: 20*time.Second,
        IdleTimeout: 60*time.Second,
    }

    log.Printf("InsightIQ API listening on :%s", cfg.Port)
    if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
        log.Fatal(err)
    }
}

func writeJSON(w http.ResponseWriter, status int, value any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(value)
}
