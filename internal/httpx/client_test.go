package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDoJSONRetriesServerErrors(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			http.Error(w, "temporary", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	var delays []time.Duration
	client := NewClient(Options{
		Sleep: func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			return nil
		},
	})
	var output struct {
		OK bool `json:"ok"`
	}

	if err := client.DoJSON(context.Background(), Request{Method: http.MethodGet, URL: server.URL}, &output, nil); err != nil {
		t.Fatal(err)
	}
	if !output.OK || attempts != 3 {
		t.Fatalf("ok=%v attempts=%d", output.OK, attempts)
	}
	if len(delays) != 2 || delays[0] != 250*time.Millisecond || delays[1] != 500*time.Millisecond {
		t.Fatalf("delays=%v", delays)
	}
}

func TestDoJSONDoesNotRetryValidationError(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		_, _ = w.Write([]byte(`{"value":""}`))
	}))
	defer server.Close()

	client := NewClient(Options{Sleep: noSleep})
	var output struct {
		Value string `json:"value"`
	}
	errInvalid := errors.New("value is required")
	err := client.DoJSON(context.Background(), Request{Method: http.MethodGet, URL: server.URL}, &output, func() error {
		if output.Value == "" {
			return errInvalid
		}
		return nil
	})
	if !errors.Is(err, errInvalid) || attempts != 1 {
		t.Fatalf("err=%v attempts=%d", err, attempts)
	}
}

func TestDoJSONAppliesRequestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewClient(Options{Timeout: 10 * time.Millisecond, MaxAttempts: 1, Sleep: noSleep})
	var output map[string]any
	err := client.DoJSON(context.Background(), Request{Method: http.MethodGet, URL: server.URL}, &output, nil)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func noSleep(context.Context, time.Duration) error { return nil }
