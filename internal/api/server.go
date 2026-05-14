package api

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/health"
	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
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

func NewServer(stateDir string, controller controllerState, health healthState, logger *slog.Logger) *Server {
	return &Server{
		socketPath: filepath.Join(stateDir, "ghr.sock"),
		controller: controller,
		health:     health,
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

	srv := &http.Server{
		Handler: s.routes(),
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		shutdownErr := srv.Close()
		cleanupErr := os.Remove(s.socketPath)
		if shutdownErr != nil {
			return fmt.Errorf("shutdown api server: %w", shutdownErr)
		}
		if cleanupErr != nil && !os.IsNotExist(cleanupErr) {
			s.logger.Warn("failed to remove socket file", "path", s.socketPath, "error", cleanupErr)
		}
		return nil
	case err := <-errCh:
		cleanupErr := os.Remove(s.socketPath)
		if cleanupErr != nil && !os.IsNotExist(cleanupErr) {
			s.logger.Warn("failed to remove socket file", "path", s.socketPath, "error", cleanupErr)
		}
		if err == http.ErrServerClosed {
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
