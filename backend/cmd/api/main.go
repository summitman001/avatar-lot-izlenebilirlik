// Komut api: HTTP sunucusu. Migration'lari CALISTIRMAZ (docker compose'ta migrate servisi yapar, `make migrate`).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/config"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/db"
	apihttp "github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/http"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/mlclient"
	"github.com/summitman001/avatar-lot-izlenebilirlik/backend/internal/repo"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("api durdu", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	conn, err := db.Open(ctx, cfg.DSN, false)
	if err != nil {
		return err
	}
	defer conn.Close()

	store := repo.New(conn).SetMLClient(mlclient.New(cfg.MLURL)).SetUploadsDir(cfg.UploadsDir)
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           apihttp.NewRouter(apihttp.Deps{DB: conn, Store: store, CORSOrigins: cfg.CORSOrigins}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		slog.Info("api dinliyor", "addr", cfg.HTTPAddr)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		slog.Info("kapatiliyor")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
	return nil
}
