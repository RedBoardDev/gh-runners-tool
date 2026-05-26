package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/health"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/logging"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/state"
)

type controllerState interface {
	Snapshots() map[string][]model.RunnerSnapshot
}

type healthState interface {
	Status() health.HealthStatus
}

type Server struct {
	socketPath string
	controller controllerState
	health     healthState
	logger     *slog.Logger
	listener   net.Listener
}

func NewServer(stateDir string, controller controllerState, healthProvider healthState, logger *slog.Logger) *Server {
	return &Server{
		socketPath: state.New(stateDir).Socket(),
		controller: controller,
		health:     healthProvider,
		logger:     logger,
	}
}

func (s *Server) Run(ctx context.Context) error {
	if err := removeStaleSocket(s.socketPath); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.socketPath, err)
	}
	s.listener = ln

	if chmodErr := os.Chmod(s.socketPath, 0o600); chmodErr != nil {
		ln.Close()
		_ = os.Remove(s.socketPath)
		return fmt.Errorf("chmod socket %s: %w", s.socketPath, chmodErr)
	}

	srv := &http.Server{
		Handler: s.routes(),
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	defer func() {
		if cleanupErr := os.Remove(s.socketPath); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
			s.logger.Warn("failed to remove socket file", logging.KeyPath, s.socketPath, logging.KeyError, cleanupErr)
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		shutdownErr := srv.Shutdown(shutdownCtx)
		<-errCh
		if shutdownErr != nil {
			return fmt.Errorf("shutdown api server: %w", shutdownErr)
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("api server: %w", err)
	}
}

func removeStaleSocket(path string) error {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat socket %s: %w", path, err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove socket %s: %w", path, err)
	}
	return nil
}
