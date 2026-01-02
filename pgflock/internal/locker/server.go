package locker

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/rickchristie/govner/pgflock/internal/config"
)

// StartServer starts the HTTP server and returns it
func StartServer(cfg *config.Config, stateUpdateChan chan<- *State) (*http.Server, *Handler, error) {
	handler := NewHandler(cfg, stateUpdateChan)

	addr := fmt.Sprintf(":%d", cfg.LockerPort)
	server := &http.Server{
		Addr:           addr,
		Handler:        handler,
		ReadTimeout:    10 * time.Minute,
		WriteTimeout:   10 * time.Minute,
		MaxHeaderBytes: 1 << 20,
	}

	go func() {
		log.Info().Str("addr", addr).Msg("Starting locker server")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("Locker server error")
		}
	}()

	return server, handler, nil
}

// StopServer gracefully shuts down the server
func StopServer(server *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Info().Msg("Shutting down locker server")
	return server.Shutdown(ctx)
}
