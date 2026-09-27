package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sdmm/internal/aphelion/collab/auth"
	"sdmm/internal/aphelion/collab/server"
	"sdmm/internal/aphelion/collab/store/postgres"
	collabtelemetry "sdmm/internal/aphelion/collab/telemetry"
)

var (
	build    = "development"
	revision = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("apheliondmm-hosted", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "hosted service YAML configuration")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *configPath == "" || flags.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "-config is required and positional arguments are forbidden")
		return 2
	}
	configFile, err := os.Open(*configPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "open hosted configuration: %v\n", err)
		return 1
	}
	config, err := server.LoadHostedConfig(configFile)
	closeErr := configFile.Close()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "load hosted configuration: %v\n", err)
		return 1
	}
	if closeErr != nil {
		_, _ = fmt.Fprintf(stderr, "close hosted configuration: %v\n", closeErr)
		return 1
	}
	databaseDSN, err := config.Database.DSN.Resolve(os.LookupEnv)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "resolve database secret: %v\n", err)
		return 1
	}
	flow, err := newHostedAuthorizationFlow(ctx, config, os.LookupEnv)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "initialize hosted authentication: %v\n", err)
		return 1
	}
	store, err := postgres.Open(ctx, postgres.Config{DSN: databaseDSN, MaxConnections: int32(config.Limits.MaxConnections), ConnectTimeout: 10 * time.Second, StatementTimeout: 5 * time.Second})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "open collaboration store: %v\n", err)
		return 1
	}
	directory := auth.NewRegistryDirectory(store)
	authentication := auth.NewManager(flow, directory, auth.ManagerConfig{SessionDirectory: directory})
	var observability *collabtelemetry.Telemetry
	var shutdownTelemetry collabtelemetry.Shutdown
	if config.Telemetry.Endpoint != "" {
		telemetryContext, cancelTelemetryInitialization := context.WithTimeout(ctx, 10*time.Second)
		observability, shutdownTelemetry, err = collabtelemetry.NewOTLP(telemetryContext, config.Telemetry.Endpoint)
		cancelTelemetryInitialization()
		if err != nil {
			_ = store.Close()
			_, _ = fmt.Fprintf(stderr, "initialize telemetry export: %v\n", err)
			return 1
		}
	}
	limits := server.DefaultLimits()
	var notificationToken string
	if config.DiscordNotifications != nil {
		notificationToken, err = config.DiscordNotifications.BotToken.Resolve(os.LookupEnv)
		if err != nil {
			_ = store.Close()
			shutdownTelemetryNow(shutdownTelemetry)
			_, _ = fmt.Fprintf(stderr, "resolve Discord notification secret: %v\n", err)
			return 1
		}
	}
	limits.MaxConnections = config.Limits.MaxConnections
	limits.MaxOperationChanges = config.Limits.MaxOperationChanges
	limits.MaxWebSocketMessageBytes = config.Limits.MaxWebSocketMessageBytes
	limits.MaxHTTPBodyBytes = config.Limits.MaxHTTPBodyBytes
	limits.MaxSnapshotBodyBytes = config.Limits.MaxSnapshotBodyBytes
	service := server.NewService(server.ServiceConfig{
		BulkSpoolBytes: config.Limits.BulkSpoolBytes, BulkWorkingBytes: config.Limits.BulkWorkingBytes,
		Store: store, Limits: limits, AllowedOrigins: []string{config.PublicOrigin}, Build: build, Revision: revision,
		HostedProvider: config.SelectedAuthProvider(), HostedPublicOrigin: config.PublicOrigin,
		HostedAuth: authentication, HostedLogin: authentication, HostedRegistry: store,
		Telemetry: observability,
		Document:  server.DocumentConfig{SnapshotOperationThreshold: 1000, SnapshotInterval: 5 * time.Minute},
	})
	if err := service.RecoverHostedSessions(ctx); err != nil {
		_ = service.Shutdown(context.Background())
		shutdownTelemetryNow(shutdownTelemetry)
		_, _ = fmt.Fprintf(stderr, "recover hosted sessions: %v\n", err)
		return 1
	}
	handler, err := server.NewTrustedProxyHandler(service.Handler(), config.TrustedProxyCIDRs)
	if err != nil {
		_ = service.Shutdown(context.Background())
		shutdownTelemetryNow(shutdownTelemetry)
		_, _ = fmt.Fprintf(stderr, "initialize trusted proxy handling: %v\n", err)
		return 1
	}
	httpServer := &http.Server{
		Addr: config.BindAddress, Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
	}
	serveErrors := make(chan error, 1)
	if config.DiscordNotifications != nil {
		notificationContext, cancelNotifications := context.WithCancel(ctx)
		defer cancelNotifications()
		go service.RunDiscordNotifications(notificationContext, *config.DiscordNotifications, notificationToken, func(err error) {
			_, _ = fmt.Fprintf(stderr, "session notifications: %v\n", err)
		})
	}
	go func() { serveErrors <- httpServer.ListenAndServe() }()
	_, _ = fmt.Fprintf(stdout, "listening=%q build=%q revision=%q\n", config.BindAddress, build, revision)
	select {
	case <-ctx.Done():
	case err := <-serveErrors:
		if err != nil && err != http.ErrServerClosed {
			_, _ = fmt.Fprintf(stderr, "serve hosted collaboration: %v\n", err)
			_ = service.Shutdown(context.Background())
			shutdownTelemetryNow(shutdownTelemetry)
			return 1
		}
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownContext); err != nil {
		_ = service.Shutdown(shutdownContext)
		shutdownTelemetryNow(shutdownTelemetry)
		_, _ = fmt.Fprintf(stderr, "shutdown HTTP server: %v\n", err)
		return 1
	}
	if err := service.Shutdown(shutdownContext); err != nil {
		shutdownTelemetryNow(shutdownTelemetry)
		_, _ = fmt.Fprintf(stderr, "shutdown collaboration service: %v\n", err)
		return 1
	}
	if shutdownTelemetry != nil {
		if err := shutdownTelemetry(shutdownContext); err != nil {
			_, _ = fmt.Fprintf(stderr, "shutdown telemetry export: %v\n", err)
			return 1
		}
	}
	return 0
}

func newHostedAuthorizationFlow(ctx context.Context, config server.HostedConfig, lookup func(string) (string, bool)) (auth.AuthorizationFlow, error) {
	switch config.SelectedAuthProvider() {
	case server.HostedProviderOIDC:
		clientSecret, err := config.OIDC.ClientSecret.Resolve(lookup)
		if err != nil {
			return nil, err
		}
		return auth.NewOIDCFlow(ctx, auth.OIDCConfig{
			Issuer: config.OIDC.Issuer, ClientID: config.OIDC.ClientID,
			ClientSecret: clientSecret, RedirectURL: config.OIDC.RedirectURL,
		})
	case server.HostedProviderDiscord:
		clientSecret, err := config.Discord.ClientSecret.Resolve(lookup)
		if err != nil {
			return nil, err
		}
		sessionTTL, err := config.Discord.SessionLifetime()
		if err != nil {
			return nil, err
		}
		return auth.NewDiscordFlow(auth.DiscordConfig{
			ClientID: config.Discord.ClientID, ClientSecret: clientSecret,
			GuildID: config.Discord.GuildID, RedirectURL: config.Discord.RedirectURL,
			SessionTTL: sessionTTL,
		})
	default:
		return nil, fmt.Errorf("auth_provider must be %q or %q", server.HostedProviderOIDC, server.HostedProviderDiscord)
	}
}

func shutdownTelemetryNow(shutdown collabtelemetry.Shutdown) {
	if shutdown == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = shutdown(ctx)
}
