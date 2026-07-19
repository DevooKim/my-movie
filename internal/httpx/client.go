package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type Request struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    any
}

type Options struct {
	HTTPClient  *http.Client
	Timeout     time.Duration
	MaxAttempts int
	Sleep       func(context.Context, time.Duration) error
}

type Client struct {
	httpClient  *http.Client
	timeout     time.Duration
	maxAttempts int
	sleep       func(context.Context, time.Duration) error
}

type StatusError struct {
	Code int
}

func (e StatusError) Error() string { return fmt.Sprintf("unexpected HTTP status %d", e.Code) }

func NewClient(options Options) *Client {
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	timeout := options.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	maxAttempts := options.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 3
	}
	sleep := options.Sleep
	if sleep == nil {
		sleep = sleepContext
	}
	return &Client{httpClient: httpClient, timeout: timeout, maxAttempts: maxAttempts, sleep: sleep}
}

func (c *Client) DoJSON(ctx context.Context, input Request, output any, validate func() error) error {
	body, err := encodeBody(input.Body)
	if err != nil {
		return err
	}

	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		err = c.doOnce(ctx, input, body, output, validate)
		if err == nil {
			return nil
		}
		if attempt == c.maxAttempts || !retryable(err) {
			return err
		}
		if err := c.sleep(ctx, retryDelay(err, attempt)); err != nil {
			return err
		}
	}
	return err
}

func (c *Client) doOnce(parent context.Context, input Request, body []byte, output any, validate func() error) error {
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, input.Method, input.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	for name, value := range input.Headers {
		request.Header.Set(name, value)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		statusErr := StatusError{Code: response.StatusCode}
		if retryAfter := response.Header.Get("Retry-After"); retryAfter != "" {
			if seconds, parseErr := strconv.Atoi(retryAfter); parseErr == nil {
				return retryAfterError{StatusError: statusErr, delay: time.Duration(seconds) * time.Second}
			}
		}
		return statusErr
	}
	if err := json.NewDecoder(response.Body).Decode(output); err != nil {
		return decodeError{err: err}
	}
	if validate != nil {
		if err := validate(); err != nil {
			return validationError{err: err}
		}
	}
	return nil
}

func encodeBody(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	return json.Marshal(value)
}

func retryable(err error) bool {
	var status StatusError
	if errors.As(err, &status) {
		return status.Code == http.StatusTooManyRequests || status.Code >= 500
	}
	var decode decodeError
	var validation validationError
	return !errors.As(err, &decode) && !errors.As(err, &validation)
}

func retryDelay(err error, attempt int) time.Duration {
	var after retryAfterError
	if errors.As(err, &after) {
		return after.delay
	}
	return time.Duration(attempt) * 250 * time.Millisecond
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type decodeError struct{ err error }

func (e decodeError) Error() string { return e.err.Error() }
func (e decodeError) Unwrap() error { return e.err }

type validationError struct{ err error }

func (e validationError) Error() string { return e.err.Error() }
func (e validationError) Unwrap() error { return e.err }

type retryAfterError struct {
	StatusError
	delay time.Duration
}
