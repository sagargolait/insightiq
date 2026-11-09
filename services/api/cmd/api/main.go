package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/sagargolait/insightiq/services/api/internal/modules/auth"
	"github.com/sagargolait/insightiq/services/api/internal/modules/dashboards"
	"github.com/sagargolait/insightiq/services/api/internal/modules/datasources"
	"github.com/sagargolait/insightiq/services/api/internal/modules/organizations"
	"github.com/sagargolait/insightiq/services/api/internal/platform/crypto"
	"github.com/sagargolait/insightiq/services/api/internal/platform/database"
	"github.com/sagargolait/insightiq/services/api/internal/platform/observability"
)

type Config struct {
	Port        string
	DatabaseURL string
	AppEnv      string
}

// loadDotenv walks up from the working directory looking for a .env file and
// loads it. This makes config work regardless of where the binary is launched
// from (repo root, services/api, etc.). A missing .env is not an error.
func loadDotenv() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for {
		path := filepath.Join(dir, ".env")
		if _, err := os.Stat(path); err == nil {
			_ = godotenv.Load(path)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return // reached filesystem root
		}
		dir = parent
	}
}

func loadConfig() Config {
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}
	return Config{
		Port:        port,
		DatabaseURL: os.Getenv("DATABASE_URL"),
		AppEnv:      os.Getenv("APP_ENV"),
	}
}

func main() {
	loadDotenv()
	cfg := loadConfig()
	logger := observability.NewLogger(cfg.AppEnv)
	slog.SetDefault(logger)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if cfg.DatabaseURL == "" {
		logger.Error("DATABASE_URL is required (see .env.example)")
		os.Exit(1)
	}

	db, err := database.NewPool(ctx, database.Config{
		URL:             cfg.DatabaseURL,
		MaxConns:        10,
		MinConns:        2,
		MaxConnLifetime: time.Hour,
		MaxConnIdleTime: 30 * time.Minute,
	})
	if err != nil {
		logger.Error("database connect failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer db.Close()

	// A superuser / BYPASSRLS connection silently disables tenant isolation.
	// Refuse to start outside development; warn loudly in development.
	if err := database.CheckRestrictedRole(ctx, db); err != nil {
		if cfg.AppEnv != "development" {
			logger.Error("database role check failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		logger.Warn("row-level security is NOT enforced; run `make db-app-role` and connect as insightiq_app",
			slog.String("error", err.Error()))
	}

	// Credential encryption for data sources. Missing keys are fatal outside
	// development; in development we warn so the rest of the API still runs,
	// and data-source routes fail closed when they try to encrypt.
	cipher, err := crypto.NewFromEnv()
	if err != nil {
		if cfg.AppEnv != "development" {
			logger.Error("encryption key config failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		logger.Warn("data source credentials cannot be encrypted; set the encryption key env vars (see .env.example)",
			slog.String("error", err.Error()),
			slog.String("keys_env", crypto.EnvKeys),
			slog.String("active_version_env", crypto.EnvActiveKeyVersion))
	}

	// Per-source connection pools for analytics queries. Closed on shutdown.
	queryExec := datasources.NewExecutor()
	defer queryExec.Close()

	// The data-source repository and service are shared by the HTTP handler and
	// the background worker (which drains the schema-discovery job queue).
	dataSourceRepo := datasources.NewPostgresRepository(db)
	dataSourceService := datasources.NewService(dataSourceRepo, cipher, datasources.NewConnector(), queryExec)

	// Background worker: runs schema discovery off the HTTP path. Tied to a
	// context cancelled on shutdown so it stops cleanly.
	workerCtx, stopWorker := context.WithCancel(context.Background())
	defer stopWorker()
	worker := datasources.NewWorker(dataSourceRepo, dataSourceService, logger)
	go worker.Run(workerCtx)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           newRouter(db, logger, datasources.NewHandler(dataSourceService)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	logger.Info("InsightIQ API listening", slog.String("port", cfg.Port), slog.String("env", cfg.AppEnv))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func newRouter(db *pgxpool.Pool, logger *slog.Logger, dataSourceHandler *datasources.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	// RequestLogger runs after RequestID so each access log carries the ID and
	// downstream handlers get a request-scoped logger on the context.
	router.Use(observability.RequestLogger(logger))
	router.Use(middleware.Recoverer)
	router.Use(middleware.Timeout(15 * time.Second))

	// Prometheus scrape endpoint. Outside /api/v1 and unauthenticated, like health.
	router.Handle("/metrics", promhttp.Handler())

	router.Get("/health/live", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	router.Get("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, stop := context.WithTimeout(r.Context(), 2*time.Second)
		defer stop()
		if err := db.Ping(pingCtx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	userRepository := auth.NewPostgresUserRepository(db)
	authService := auth.NewAuthService(userRepository)
	authHandler := auth.NewHandler(authService)

	orgHandler := organizations.NewHandler(
		organizations.NewService(organizations.NewPostgresRepository(db)),
	)

	dashboardHandler := dashboards.NewHandler(
		dashboards.NewService(dashboards.NewPostgresRepository(db)),
	)

	router.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/register", authHandler.Register)
		r.Post("/auth/login", authHandler.Login)
		r.Post("/auth/logout", authHandler.Logout)

		r.Group(func(r chi.Router) {
			r.Use(authHandler.RequireSession)
			r.Get("/auth/me", authHandler.Me)
			r.Mount("/orgs", orgHandler.Routes())
			// Dashboards and widgets are organization-scoped; the handler
			// reads orgID from this parent route and uses it as the RLS tenant.
			r.Mount("/orgs/{orgID}/dashboards", dashboardHandler.Routes())
			r.Mount("/orgs/{orgID}/data-sources", dataSourceHandler.Routes())
			r.Post("/invitations/accept", orgHandler.AcceptInvitation)
		})
	})

	return router
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
