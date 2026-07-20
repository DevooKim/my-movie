# Provider Scoped Backoff Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Apply the six-minute burst backoff only to Megabox fetch failures.

**Architecture:** Wrap fetch failures in a scheduler-local error that retains the provider ID. The scheduler's next-boundary selection searches joined errors for that marker and delays only when the failed provider is Megabox.

**Tech Stack:** Go, standard library errors and testing.

---

### Task 1: Retain fetch-provider identity for backoff

**Files:**
- Modify: `internal/scheduler/scheduler_test.go`
- Modify: `internal/scheduler/scheduler.go`
- Modify: `README.md`

- [ ] **Step 1: Write failing backoff tests**

```go
func TestNextBurstBoundaryDoesNotBackoffCGVFetchFailure(t *testing.T) {
    now := time.Date(2026, 7, 20, 12, 0, 16, 0, time.UTC)
    runErr := providerFetchError{provider: domain.ProviderCGV, err: errors.New("unexpected EOF")}
    if got := nextBurstBoundary(now, time.Minute, runErr); !got.Equal(time.Date(2026, 7, 20, 12, 1, 0, 0, time.UTC)) {
        t.Fatalf("boundary=%s", got)
    }
}
```

- [ ] **Step 2: Run the test and verify it fails**

Run: `go test ./internal/scheduler -run TestNextBurstBoundaryDoesNotBackoffCGVFetchFailure -count=1`

Expected: FAIL because all errors currently trigger the six-minute delay.

- [ ] **Step 3: Implement provider-scoped classification**

```go
type providerFetchError struct {
    provider domain.ProviderID
    err error
}

func (e providerFetchError) Error() string { return e.err.Error() }
func (e providerFetchError) Unwrap() error { return e.err }
```

Wrap failed `pollBranchWithFetch` results with this type and make `nextBurstBoundary` use the six-minute delay only when `errors.As` finds a Megabox marker.

- [ ] **Step 4: Run scheduler and full tests**

Run: `go test ./internal/scheduler -count=1 && go test ./...`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/scheduler/scheduler.go internal/scheduler/scheduler_test.go README.md docs/superpowers/specs/2026-07-20-provider-scoped-backoff-design.md docs/superpowers/plans/2026-07-20-provider-scoped-backoff.md
git commit -m "fix: 메가박스 오류에만 조회 대기 적용"
```
