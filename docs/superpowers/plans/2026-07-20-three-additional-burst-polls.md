# Three Additional Burst Polls Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make four polling attempts per one-minute boundary burst at five-second intervals.

**Architecture:** Expand the scheduler's fixed burst window from zero to fifteen seconds. Existing per-attempt snapshot recording and pending delivery execute after every provider result, so no notification flow changes are needed.

**Tech Stack:** Go, standard library testing.

---

### Task 1: Expand the burst offsets

**Files:**
- Modify: `internal/scheduler/scheduler_test.go`
- Modify: `internal/scheduler/scheduler.go`
- Modify: `README.md`

- [ ] **Step 1: Write the failing offset test**

```go
func TestBurstOffsetsIncludeThreeAdditionalFiveSecondAttempts(t *testing.T) {
    want := []time.Duration{0, 5 * time.Second, 10 * time.Second, 15 * time.Second}
    if got := burstOffsets(); !slices.Equal(got, want) {
        t.Fatalf("offsets=%v", got)
    }
}
```

- [ ] **Step 2: Run the test and verify it fails**

Run: `go test ./internal/scheduler -run TestBurstOffsetsIncludeThreeAdditionalFiveSecondAttempts -count=1`

Expected: FAIL because the current burst has only offset `0`.

- [ ] **Step 3: Change the fixed burst window**

```go
const burstWindow = 15 * time.Second
```

Update the existing burst execution test to require four CGV fetches, four Megabox fetches, and delivery after every provider result.

- [ ] **Step 4: Run scheduler tests and build**

Run: `go test ./internal/scheduler -count=1 && docker build --no-cache -t my-movie-burst-check .`

Expected: PASS and service binaries compile.

- [ ] **Step 5: Commit**

```bash
git add internal/scheduler/scheduler.go internal/scheduler/scheduler_test.go README.md docs/superpowers/specs/2026-07-20-three-additional-burst-polls-design.md docs/superpowers/plans/2026-07-20-three-additional-burst-polls.md
git commit -m "feat: 분당 추가 버스트 조회"
```
