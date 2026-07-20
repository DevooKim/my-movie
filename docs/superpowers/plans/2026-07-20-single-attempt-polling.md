# Single Attempt Polling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent duplicate external HTTP requests within a one-minute polling cycle.

**Architecture:** Keep retry support in the reusable HTTP client for explicitly configured callers. Configure the production app and provider smoke command with one maximum attempt, and protect the default through a focused HTTP-client regression test.

**Tech Stack:** Go, net/http, httptest.

---

### Task 1: Make the default client single-attempt

**Files:**
- Modify: `internal/httpx/client_test.go`
- Modify: `internal/httpx/client.go`

- [ ] **Step 1: Write the failing test**

```go
func TestDoJSONDefaultClientDoesNotRetryServerErrors(t *testing.T) {
    attempts := 0
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
        attempts++
        http.Error(w, "temporary", http.StatusInternalServerError)
    }))
    defer server.Close()

    var output map[string]any
    err := NewClient(Options{Sleep: noSleep}).DoJSON(context.Background(), Request{Method: http.MethodGet, URL: server.URL}, &output, nil)
    if err == nil || attempts != 1 {
        t.Fatalf("err=%v attempts=%d", err, attempts)
    }
}
```

- [ ] **Step 2: Run the test and verify it fails**

Run: `go test ./internal/httpx -run TestDoJSONDefaultClientDoesNotRetryServerErrors -count=1`

Expected: FAIL because the default client makes three attempts.

- [ ] **Step 3: Write the minimal implementation**

```go
maxAttempts := options.MaxAttempts
if maxAttempts == 0 {
    maxAttempts = 1
}
```

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/httpx -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/httpx/client.go internal/httpx/client_test.go docs/superpowers/specs/2026-07-20-single-attempt-polling-design.md docs/superpowers/plans/2026-07-20-single-attempt-polling.md
git commit -m "fix: 폴링 요청 재시도 제거"
```
