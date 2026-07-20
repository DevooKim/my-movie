# Burst Error Backoff Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delay the next poll by six minutes when a poll burst fails.

**Architecture:** Keep scheduling in `Scheduler.Start` and extract the next-boundary decision into a pure helper. Successful bursts use the regular interval boundary; reportable burst failures use a fixed six-minute delay.

**Tech Stack:** Go, standard library testing.

---

### Task 1: Select the next boundary after a burst

**Files:**
- Modify: `internal/scheduler/scheduler_test.go`
- Modify: `internal/scheduler/scheduler.go`

- [ ] **Step 1: Write the failing test**

```go
func TestNextBurstBoundaryWaitsSixMinutesAfterFailure(t *testing.T) {
    now := time.Date(2026, 7, 20, 12, 0, 2, 0, time.UTC)
    if got := nextBurstBoundary(now, time.Minute, errors.New("upstream failed")); !got.Equal(now.Add(6*time.Minute)) {
        t.Fatalf("boundary=%s", got)
    }
}
```

- [ ] **Step 2: Run the test and verify it fails**

Run: `go test ./internal/scheduler -run TestNextBurstBoundaryWaitsSixMinutesAfterFailure -count=1`

Expected: FAIL because `nextBurstBoundary` is undefined.

- [ ] **Step 3: Implement the boundary helper**

```go
const burstErrorBackoff = 6 * time.Minute

func nextBurstBoundary(now time.Time, interval time.Duration, runErr error) time.Time {
    if runErr != nil && !errors.Is(runErr, ErrCycleRunning) && !errors.Is(runErr, context.Canceled) {
        return now.Add(burstErrorBackoff)
    }
    return burstBoundary(now.Add(time.Nanosecond), interval)
}
```

Use the helper after every `runBurst` call in `Scheduler.Start`.

- [ ] **Step 4: Run scheduler and full tests**

Run: `go test ./internal/scheduler -count=1 && go test ./...`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/scheduler/scheduler.go internal/scheduler/scheduler_test.go docs/superpowers/specs/2026-07-20-burst-error-backoff-design.md docs/superpowers/plans/2026-07-20-burst-error-backoff.md
git commit -m "fix: 버스트 오류 후 조회 대기"
```
