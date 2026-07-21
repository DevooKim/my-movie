# Three Minute Fifteen Second Burst Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Poll every three minutes with four attempts spaced fifteen seconds apart.

**Architecture:** Change the configuration default from 60 to 180 seconds and expand the scheduler's fixed step/window to 15/45 seconds. Existing provider preparation, snapshot comparison, immediate delivery, and error classification remain unchanged.

**Tech Stack:** Go, standard library testing, Docker Compose configuration.

---

### Task 1: Change the polling cadence

**Files:**
- Modify: `internal/config/config_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/scheduler/scheduler_test.go`
- Modify: `internal/scheduler/scheduler.go`
- Modify: `.env.example`
- Modify: `README.md`

- [ ] **Step 1: Write failing cadence tests**

```go
if cfg.PollInterval != 3*time.Minute {
    t.Fatalf("interval=%s", cfg.PollInterval)
}

want := []time.Duration{0, 15*time.Second, 30*time.Second, 45*time.Second}
```

- [ ] **Step 2: Run tests and verify they fail**

Run: `go test ./internal/config ./internal/scheduler -count=1`

Expected: FAIL because defaults are 60 seconds and burst offsets are 0/5/10/15 seconds.

- [ ] **Step 3: Implement the new cadence**

```go
const defaultPollSeconds = 180

const (
    burstStep = 15 * time.Second
    burstWindow = 45 * time.Second
)
```

Update `.env.example` and README to use `POLL_INTERVAL_SECONDS=180` and describe `+0/+15/+30/+45초`.

- [ ] **Step 4: Run tests and build**

Run: `go test ./... && docker build --no-cache -t my-movie-three-minute-check .`

Expected: PASS and all service binaries compile.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/scheduler/scheduler.go internal/scheduler/scheduler_test.go .env.example README.md docs/superpowers/specs/2026-07-21-three-minute-fifteen-second-burst-design.md docs/superpowers/plans/2026-07-21-three-minute-fifteen-second-burst.md
git commit -m "config: 3분 주기 15초 버스트 조회"
```
