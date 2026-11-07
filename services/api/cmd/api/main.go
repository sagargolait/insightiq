package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/sagargolait/insightiq/services/api/internal/modules/auth"
	"github.com/sagargolait/insightiq/services/api/internal/modules/organizations"
	"github.com/sagargolait/insightiq/services/api/internal/platform/database"
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if cfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL is required (see .env.example)")
	}

	db, err := database.NewPool(ctx, database.Config{
		URL:             cfg.DatabaseURL,
		MaxConns:        10,
		MinConns:        2,
		MaxConnLifetime: time.Hour,
		MaxConnIdleTime: 30 * time.Minute,
	})
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()

	// A superuser / BYPASSRLS connection silently disables tenant isolation.
	// Refuse to start outside development; warn loudly in development.
	if err := database.CheckRestrictedRole(ctx, db); err != nil {
		if cfg.AppEnv != "development" {
			log.Fatalf("database: %v", err)
		}
		log.Printf("WARNING: %v — row-level security is NOT enforced. "+
			"Run `make db-app-role` and connect as insightiq_app.", err)
	}

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           newRouter(db),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("InsightIQ API listening on :%s", cfg.Port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func newRouter(db *pgxpool.Pool) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)
	router.Use(middleware.Timeout(15 * time.Second))

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

	router.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/register", authHandler.Register)
		r.Post("/auth/login", authHandler.Login)
		r.Post("/auth/logout", authHandler.Logout)

		r.Group(func(r chi.Router) {
			r.Use(authHandler.RequireSession)
			r.Get("/auth/me", authHandler.Me)
			r.Mount("/orgs", orgHandler.Routes())
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
