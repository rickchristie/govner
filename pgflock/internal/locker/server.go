package locker

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/rickchristie/govner/pgflock/internal/config"
)

// StartServer starts the HTTP server and returns it along with an error channel.
// The error channel receives any errors that occur during server operation (e.g., if the server dies).
// The channel is buffered (size 5) to prevent blocking.
func StartServer(cfg *config.Config, stateUpdateChan chan<- *State) (*http.Server, *Handler, <-chan error, error) {
	handler := NewHandler(cfg, stateUpdateChan)

	addr := fmt.Sprintf(":%d", cfg.LockerPort)

	// Try to bind to the port first to catch "address already in use" errors synchronously
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to bind to port %d: %w", cfg.LockerPort, err)
	}

	server := &http.Server{
		Handler:        handler,
		ReadTimeout:    10 * time.Minute,
		WriteTimeout:   10 * time.Minute,
		MaxHeaderBytes: 1 << 20,
	}

	// Buffered error channel for runtime errors
	errChan := make(chan error, 5)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("locker server panic: %v", r)
				log.Error().Err(err).Msg("Locker server panic recovered")
				select {
				case errChan <- err:
				default:
				}
			}
		}()

		log.Info().Str("addr", addr).Msg("Starting locker server")
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("Locker server error")
			select {
			case errChan <- err:
			default:
			}
		}
	}()

	return server, handler, errChan, nil
}

// StopServer gracefully shuts down the server
func StopServer(server *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Info().Msg("Shutting down locker server")
	return server.Shutdown(ctx)
}
