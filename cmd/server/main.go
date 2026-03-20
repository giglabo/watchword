package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/mark3labs/mcp-go/server"

	"github.com/watchword/watchword/internal/auth"
	"github.com/watchword/watchword/internal/config"
	"github.com/watchword/watchword/internal/health"
	mcpserver "github.com/watchword/watchword/internal/mcp"
	"github.com/watchword/watchword/internal/proxy"
	"github.com/watchword/watchword/internal/repository"
	s3client "github.com/watchword/watchword/internal/s3"
	"github.com/watchword/watchword/internal/service"
	"github.com/watchword/watchword/internal/worker"
)

const version = "1.0.0"

func main() {
	configPath := flag.String("config", "", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger := setupLogger(cfg.Logging)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	repo, err := initRepo(ctx, cfg, logger)
	if err != nil {
		logger.Error("failed to initialize repository", "error", err)
		os.Exit(1)
	}
	defer repo.Close()

	if err := repo.Migrate(ctx); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	authenticator := auth.NewAuthenticator(cfg.Auth.Enabled, cfg.Auth.Tokens)

	if cfg.Auth.JWT != nil {
		jwtVal, err := auth.NewJWTValidator(ctx, cfg.Auth.JWT)
		if err != nil {
			logger.Error("failed to initialize JWT validator", "error", err)
			os.Exit(1)
		}
		defer jwtVal.Close()
		authenticator.SetJWTValidator(jwtVal)
		logger.Info("JWT validation enabled", "jwks_url", cfg.Auth.JWT.JWKSURL)
	}

	if cfg.Auth.ResourceMetadata != nil {
		rmURL := cfg.Auth.ResourceMetadata.Resource + "/.well-known/oauth-protected-resource"
		authenticator.SetResourceMetadataURL(rmURL)
		logger.Info("protected resource metadata enabled",
			"resource", cfg.Auth.ResourceMetadata.Resource,
			"authorization_servers", cfg.Auth.ResourceMetadata.AuthorizationServers)
	}

	if cfg.Server.Transport == "stdio" && cfg.Auth.Enabled {
		token := os.Getenv("WORDSTORE_AUTH_TOKEN")
		if err := authenticator.Validate(token); err != nil {
			logger.Error("stdio auth failed: invalid WORDSTORE_AUTH_TOKEN")
			os.Exit(1)
		}
		logger.Info("stdio auth validated successfully")
	}

	svc := service.NewEntryService(repo, cfg.Expiration.TTLHours, logger)

	var fileSvc *service.FileService
	var s3c *s3client.Client
	if cfg.S3 != nil {
		var err error
		s3c, err = s3client.NewClient(ctx, cfg.S3)
		if err != nil {
			logger.Error("failed to initialize S3 client", "error", err)
			os.Exit(1)
		}
		fileSvc = service.NewFileService(repo, s3c, cfg.Expiration.TTLHours, cfg.S3.MaxFileSizeBytes, logger)
		logger.Info("S3 file storage enabled", "bucket", cfg.S3.Bucket, "region", cfg.S3.Region)

		if cfg.S3.Proxy != nil {
			signer := proxy.NewURLSigner(cfg.S3.Proxy.BaseURL, cfg.S3.Proxy.HMACSecret, cfg.S3.Proxy.URLTTLMinutes)
			fileSvc.SetProxySigner(signer)
			logger.Info("proxy download enabled", "base_url", cfg.S3.Proxy.BaseURL, "ttl_minutes", cfg.S3.Proxy.URLTTLMinutes)
		}
	}

	mcpSrv := mcpserver.NewServer(svc, fileSvc, cfg.Tools, logger)

	if cfg.Expiration.Enabled {
		historyRetention := 90 // default
		if cfg.S3 != nil && cfg.S3.Proxy != nil {
			historyRetention = cfg.S3.Proxy.HistoryRetentionDays
		}
		w := worker.NewExpirationWorker(repo, cfg.Expiration.IntervalHours, historyRetention, logger)
		go w.Start(ctx)
		logger.Info("expiration worker started", "interval_hours", cfg.Expiration.IntervalHours)
	}

	// Start health/status HTTP server
	healthSrv := startHealthServer(cfg, repo, logger, ctx)

	logger.Info("starting server", "transport", cfg.Server.Transport)

	switch cfg.Server.Transport {
	case "stdio":
		stdio := server.NewStdioServer(mcpSrv)
		if err := stdio.Listen(ctx, os.Stdin, os.Stdout); err != nil {
			logger.Error("stdio server error", "error", err)
			os.Exit(1)
		}
	case "sse":
		sseSrv := server.NewSSEServer(mcpSrv,
			server.WithSSEEndpoint("/sse"),
			server.WithBaseURL(fmt.Sprintf("http://localhost:%d", cfg.Server.SSEPort)),
		)
		innerMux := http.NewServeMux()
		innerMux.Handle("/sse", sseSrv.SSEHandler())
		innerMux.Handle("/message", sseSrv.MessageHandler())
		registerOAuthMetadata(innerMux, cfg)
		outerMux := http.NewServeMux()
		registerProxyHandler(outerMux, cfg, s3c, repo, logger)
		outerMux.Handle("/", wrapWithAuth(innerMux, authenticator, cfg))
		addr := fmt.Sprintf(":%d", cfg.Server.SSEPort)
		httpSrv := &http.Server{Addr: addr, Handler: outerMux}
		go func() {
			<-ctx.Done()
			httpSrv.Shutdown(context.Background())
		}()
		logger.Info("SSE server listening", "addr", addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("SSE server error", "error", err)
			os.Exit(1)
		}
	case "streamable-http":
		innerMux := http.NewServeMux()
		innerMux.Handle("/mcp", server.NewStreamableHTTPServer(mcpSrv))
		registerOAuthMetadata(innerMux, cfg)
		outerMux := http.NewServeMux()
		registerProxyHandler(outerMux, cfg, s3c, repo, logger)
		outerMux.Handle("/", wrapWithAuth(innerMux, authenticator, cfg))
		addr := fmt.Sprintf(":%d", cfg.Server.HTTPPort)
		httpSrv := &http.Server{Addr: addr, Handler: outerMux}
		go func() {
			<-ctx.Done()
			httpSrv.Shutdown(context.Background())
		}()
		logger.Info("Streamable HTTP server listening", "addr", addr, "endpoint", "/mcp")
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Streamable HTTP server error", "error", err)
			os.Exit(1)
		}
	case "http":
		// Serve both SSE and Streamable HTTP on the same port
		innerMux := http.NewServeMux()
		innerMux.Handle("/mcp", server.NewStreamableHTTPServer(mcpSrv))
		sseSrv := server.NewSSEServer(mcpSrv,
			server.WithSSEEndpoint("/sse"),
			server.WithBaseURL(fmt.Sprintf("http://localhost:%d", cfg.Server.HTTPPort)),
		)
		innerMux.Handle("/sse", sseSrv.SSEHandler())
		innerMux.Handle("/message", sseSrv.MessageHandler())
		registerOAuthMetadata(innerMux, cfg)
		outerMux := http.NewServeMux()
		registerProxyHandler(outerMux, cfg, s3c, repo, logger)
		outerMux.Handle("/", wrapWithAuth(innerMux, authenticator, cfg))
		httpSrv := &http.Server{
			Addr:    fmt.Sprintf(":%d", cfg.Server.HTTPPort),
			Handler: outerMux,
		}
		go func() {
			<-ctx.Done()
			httpSrv.Shutdown(context.Background())
		}()
		logger.Info("HTTP server listening (SSE + Streamable HTTP)", "addr", httpSrv.Addr, "sse", "/sse", "streamable-http", "/mcp")
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server error", "error", err)
			os.Exit(1)
		}
	}

	if healthSrv != nil {
		healthSrv.Shutdown(context.Background())
	}

	logger.Info("server shut down gracefully")
}

func startHealthServer(cfg *config.Config, repo repository.Repository, logger *slog.Logger, ctx context.Context) *http.Server {
	if cfg.Server.HealthPort == 0 {
		return nil
	}

	h := health.NewServer(repo, logger, version, cfg.Database.Driver)
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.HealthPort),
		Handler: h.Handler(),
	}

	go func() {
		logger.Info("health server listening", "port", cfg.Server.HealthPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("health server error", "error", err)
		}
	}()

	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()

	return srv
}

func setupLogger(cfg config.LoggingConfig) *slog.Logger {
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	var w *os.File
	if cfg.File != "" {
		f, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to open log file %s: %v, falling back to stderr\n", cfg.File, err)
			w = os.Stderr
		} else {
			w = f
		}
	} else {
		w = os.Stderr
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.Format == "text" {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}

	return slog.New(handler)
}

func initRepo(ctx context.Context, cfg *config.Config, logger *slog.Logger) (repository.Repository, error) {
	switch cfg.Database.Driver {
	case "sqlite":
		logger.Info("initializing SQLite", "path", cfg.Database.SQLite.Path)
		if err := os.MkdirAll(dataDir(cfg.Database.SQLite.Path), 0755); err != nil {
			return nil, fmt.Errorf("creating data directory: %w", err)
		}
		return repository.NewSQLiteRepo(cfg.Database.SQLite.Path)
	case "postgres":
		logger.Info("initializing PostgreSQL")
		return repository.NewPostgresRepo(ctx, cfg.Database.Postgres.DSN)
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", cfg.Database.Driver)
	}
}

func registerOAuthMetadata(mux *http.ServeMux, cfg *config.Config) {
	// RFC 9728: Protected Resource Metadata (draft MCP spec, MUST for OAuth)
	if cfg.Auth.ResourceMetadata != nil {
		mux.Handle("/.well-known/oauth-protected-resource",
			auth.ProtectedResourceMetadataHandler(cfg.Auth.ResourceMetadata))
	}
	// RFC 8414: Authorization Server Metadata (legacy 2025-03-26 MCP spec compat)
	if cfg.Auth.JWT != nil && cfg.Auth.OAuthMetadata != nil {
		mux.Handle("/.well-known/oauth-authorization-server",
			auth.OAuthMetadataHandler(cfg.Auth.JWT.JWKSURL, cfg.Auth.JWT.Issuer, cfg.Auth.OAuthMetadata))
	}
}

func registerProxyHandler(outerMux *http.ServeMux, cfg *config.Config, s3c s3client.Streamer, repo repository.Repository, logger *slog.Logger) {
	if cfg.S3 != nil && cfg.S3.Proxy != nil && s3c != nil {
		dlHandler := proxy.NewHandler(cfg.S3.Proxy.HMACSecret, s3c, repo, logger)
		outerMux.Handle("/dl", dlHandler)

		ulHandler := proxy.NewUploadHandler(cfg.S3.Proxy.HMACSecret, s3c, repo, cfg.S3.MaxFileSizeBytes, logger)
		outerMux.Handle("/ul", ulHandler)
	}
}

func wrapWithAuth(mux *http.ServeMux, authenticator *auth.Authenticator, cfg *config.Config) http.Handler {
	if cfg.Auth.Enabled {
		return authenticator.HTTPMiddleware(mux)
	}
	return mux
}

func dataDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}
