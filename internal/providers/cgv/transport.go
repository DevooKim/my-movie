package cgv

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const cgvOrigin = "https://cgv.co.kr"

type cdpTransport struct{ endpoint string }

func newCDPTransport(endpoint string) *cdpTransport {
	if endpoint == "" {
		endpoint = "http://lightpanda:9222"
	}
	return &cdpTransport{endpoint: endpoint}
}

func (t *cdpTransport) dates(ctx context.Context, theaterID string) ([]string, error) {
	values := url.Values{"coCd": {"A420"}, "siteNo": {theaterID}}
	var response apiResponse[[]dateResponse]
	if err := t.get(ctx, "/api/v1/booking/searchSiteScnscYmdListBySite?"+values.Encode(), &response); err != nil {
		return nil, err
	}
	if response.StatusCode != 0 {
		return nil, fmt.Errorf("cgv date response: %s (%d)", response.StatusMessage, response.StatusCode)
	}
	result := make([]string, 0, len(response.Data))
	seen := map[string]bool{}
	for _, row := range response.Data {
		if row.ScnYmd != "" && !seen[row.ScnYmd] {
			result = append(result, row.ScnYmd)
			seen[row.ScnYmd] = true
		}
	}
	return result, nil
}

func (t *cdpTransport) showtimes(ctx context.Context, theaterID, playDate string) ([]showtimeResponse, error) {
	values := url.Values{"coCd": {"A420"}, "siteNo": {theaterID}, "scnYmd": {playDate}, "rtctlScopCd": {"08"}}
	var response apiResponse[[]showtimeResponse]
	if err := t.get(ctx, "/api/v1/booking/searchMovScnInfo?"+values.Encode(), &response); err != nil {
		return nil, err
	}
	if response.StatusCode != 0 {
		return nil, fmt.Errorf("cgv showtime response: %s (%d)", response.StatusMessage, response.StatusCode)
	}
	return response.Data, nil
}

func (t *cdpTransport) get(ctx context.Context, path string, output any) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
	}
	wsURL, err := websocketURL(t.endpoint)
	if err != nil {
		return err
	}
	connection, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("connect Lightpanda CDP: %w", err)
	}
	defer connection.Close()
	client := &cdpClient{connection: connection}
	var created struct {
		TargetID string `json:"targetId"`
	}
	if err := client.call(ctx, "Target.createTarget", map[string]any{"url": cgvOrigin}, "", &created); err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.call(closeCtx, "Target.closeTarget", map[string]any{"targetId": created.TargetID}, "", &struct{}{})
	}()
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := client.call(ctx, "Target.attachToTarget", map[string]any{"targetId": created.TargetID, "flatten": true}, "", &attached); err != nil {
		return err
	}
	if err := client.call(ctx, "Page.enable", nil, attached.SessionID, &struct{}{}); err != nil {
		return err
	}
	if err := client.call(ctx, "Page.navigate", map[string]any{"url": cgvOrigin}, attached.SessionID, &struct{}{}); err != nil {
		return err
	}
	expression := "fetch(" + strconvQuote(cgvOrigin+path) + ").then(async r => { if (!r.ok) throw new Error('HTTP ' + r.status); return await r.text(); })"
	var evaluated struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	var evaluateErr error
	for attempt := 0; attempt < 10; attempt++ {
		evaluateErr = client.call(ctx, "Runtime.evaluate", map[string]any{"expression": expression, "awaitPromise": true, "returnByValue": true}, attached.SessionID, &evaluated)
		if evaluateErr == nil || !strings.Contains(evaluateErr.Error(), "Cannot find context") {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	if evaluateErr != nil {
		return evaluateErr
	}
	if len(evaluated.ExceptionDetails) != 0 {
		return fmt.Errorf("CGV request failed in Lightpanda: %s", evaluated.ExceptionDetails)
	}
	var body string
	if err := json.Unmarshal(evaluated.Result.Value, &body); err == nil {
		if err := json.Unmarshal([]byte(body), output); err != nil {
			return fmt.Errorf("decode CGV response: %w", err)
		}
		return nil
	}
	if err := json.Unmarshal(evaluated.Result.Value, output); err != nil {
		return fmt.Errorf("decode CGV response: %w", err)
	}
	return nil
}

func websocketURL(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse Lightpanda URL: %w", err)
	}
	if parsed.Scheme == "http" {
		parsed.Scheme = "ws"
	} else if parsed.Scheme == "https" {
		parsed.Scheme = "wss"
	}
	if parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return "", fmt.Errorf("Lightpanda URL must use http or https")
	}
	parsed.Path = "/"
	parsed.RawQuery = ""
	return parsed.String(), nil
}
func strconvQuote(value string) string { encoded, _ := json.Marshal(value); return string(encoded) }

type cdpClient struct {
	connection *websocket.Conn
	nextID     atomic.Int64
}
type cdpMessage struct {
	ID        int64           `json:"id"`
	Method    string          `json:"method"`
	SessionID string          `json:"sessionId"`
	Result    json.RawMessage `json:"result"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *cdpClient) call(ctx context.Context, method string, params any, sessionID string, output any) error {
	id := c.nextID.Add(1)
	request := map[string]any{"id": id, "method": method}
	if params != nil {
		request["params"] = params
	}
	if sessionID != "" {
		request["sessionId"] = sessionID
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.connection.SetWriteDeadline(deadline)
		_ = c.connection.SetReadDeadline(deadline)
	}
	if err := c.connection.WriteJSON(request); err != nil {
		return fmt.Errorf("CDP %s: %w", method, err)
	}
	for {
		var message cdpMessage
		if err := c.connection.ReadJSON(&message); err != nil {
			return fmt.Errorf("CDP %s: %w", method, err)
		}
		if message.ID != id {
			continue
		}
		if message.Error != nil {
			return fmt.Errorf("CDP %s: %s", method, message.Error.Message)
		}
		if output != nil && len(message.Result) > 0 {
			if err := json.Unmarshal(message.Result, output); err != nil {
				return fmt.Errorf("decode CDP %s: %w", method, err)
			}
		}
		return nil
	}
}
