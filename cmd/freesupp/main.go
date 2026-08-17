// Command freesupp runs the FreeSupp support server.
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

	"github.com/kooler/freesupp/internal/captcha"
	"github.com/kooler/freesupp/internal/config"
	"github.com/kooler/freesupp/internal/mail"
	"github.com/kooler/freesupp/internal/server"
	"github.com/kooler/freesupp/internal/store"
)

// notifyDrainTimeout bounds how long shutdown waits for emails still in flight.
const notifyDrainTimeout = 15 * time.Second

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		log.Error("configuration error", "err", err)
		os.Exit(1)
	}

	if err := run(cfg, log); err != nil {
		log.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func run(cfg *config.Config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(context.Background(), cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	notifier := mail.NewNotifier(cfg, mail.NewSender(cfg, log), log)
	defer func() {
		if !notifier.WaitTimeout(notifyDrainTimeout) {
			log.Warn("gave up on in-flight email notifications", "timeout", notifyDrainTimeout)
		}
	}()

	srv := &http.Server{
		Addr: cfg.Listen,
		Handler: server.New(cfg, log, server.Deps{
			Store:    st,
			Notifier: notifier,
			Verifier: captcha.New(cfg),
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Listen, "base_url", cfg.BaseURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
		close(errc)
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
