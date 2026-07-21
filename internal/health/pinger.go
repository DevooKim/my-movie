package health

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type Pinger struct {
	url      string
	interval time.Duration
	client   *http.Client
}

func NewPinger(url string, interval time.Duration) *Pinger {
	if url == "" {
		return nil
	}
	return &Pinger{
		url:      url,
		interval: interval,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *Pinger) Start(ctx context.Context) {
	go func() {
		p.ping(ctx)
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.ping(ctx)
			}
		}
	}()
}

func (p *Pinger) ping(ctx context.Context) {
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, p.url, nil)
	if err != nil {
		slog.Warn("healthcheck ping failed", "error", err)
		return
	}
	response, err := p.client.Do(request)
	if err != nil {
		slog.Warn("healthcheck ping failed", "error", err)
		return
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		slog.Warn("healthcheck ping failed", "error", fmt.Sprintf("HTTP %d", response.StatusCode))
	}
}
