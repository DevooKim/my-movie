package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"my-movie/internal/domain"
)

type Store interface {
	PingContext(context.Context) error
	ListActiveProviderIDs(context.Context) ([]domain.ProviderID, error)
	LatestSuccessfulPoll(context.Context, domain.ProviderID) (time.Time, error)
}

type Handler struct {
	store    Store
	interval time.Duration
	now      func() time.Time
}

func NewHandler(store Store, interval time.Duration, now func() time.Time) *Handler {
	return &Handler{store: store, interval: interval, now: now}
}

func (h *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/health" {
		http.NotFound(writer, request)
		return
	}
	status := "ok"
	statusCode := http.StatusOK
	if err := h.check(request.Context()); err != nil {
		status = "unavailable"
		statusCode = http.StatusServiceUnavailable
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(map[string]string{"status": status})
}

func (h *Handler) check(ctx context.Context) error {
	if err := h.store.PingContext(ctx); err != nil {
		return err
	}
	providers, err := h.store.ListActiveProviderIDs(ctx)
	if err != nil {
		return err
	}
	oldestAllowed := h.now().Add(-2 * h.interval)
	for _, provider := range providers {
		latest, err := h.store.LatestSuccessfulPoll(ctx, provider)
		if err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("provider has no successful poll")
			}
			return err
		}
		if latest.Before(oldestAllowed) {
			return fmt.Errorf("provider poll is stale")
		}
	}
	return nil
}

type Server struct {
	server   *http.Server
	listener net.Listener
}

func NewServer(port int, handler http.Handler) *Server {
	return &Server{server: &http.Server{
		Addr: fmt.Sprintf(":%d", port), Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
	}}
}

func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		return err
	}
	s.listener = listener
	go func() { _ = s.server.Serve(listener) }()
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error { return s.server.Shutdown(ctx) }
