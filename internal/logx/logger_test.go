package logx

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestNewWritesOneJSONRecordPerEvent(t *testing.T) {
	var output bytes.Buffer
	logger := New(&output)

	logger.InfoContext(context.Background(), "poll completed", "provider", "megabox")

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if record["msg"] != "poll completed" {
		t.Fatalf("msg=%v", record["msg"])
	}
	if record["provider"] != "megabox" {
		t.Fatalf("provider=%v", record["provider"])
	}
}
