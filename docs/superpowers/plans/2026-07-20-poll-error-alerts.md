# Poll Error Alerts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Notify the Discord control channel once when a provider branch starts failing and once when it recovers, without duplicate alerts across polling cycles or restarts.

**Architecture:** Persist a per-provider/per-theater health state in SQLite. A scheduler-injected reporter receives only fetch outcomes; it sends the transition message to the saved control channel and commits the new state only after Discord accepts the message. The existing booking-alert channels and pending delivery workflow remain unchanged.

**Tech Stack:** Go 1.26, SQLite migrations, discordgo, Go `testing`

---

### Task 1: Persist provider-branch alert state

**Files:**
- Create: `internal/database/migrations/005_poll_alert_states.sql`
- Modify: `internal/database/repository.go`
- Modify: `internal/database/repository_test.go`

- [ ] **Step 1: Write failing repository tests**

```go
func TestPollAlertStatePersistsAcrossReopen(t *testing.T) {
	// Save PollAlertState{Provider: domain.ProviderCGV, TheaterID: "0013", Failed: true}
	// Reopen the database and assert the same state is returned.
}
```

- [ ] **Step 2: Run the focused test to verify RED**

Run: `go test ./internal/database -run TestPollAlertStatePersistsAcrossReopen -v`

Expected: compilation fails because `PollAlertState`, `GetPollAlertState`, and `SavePollAlertState` do not exist.

- [ ] **Step 3: Add migration and repository methods**

```sql
CREATE TABLE poll_alert_states (
  provider TEXT NOT NULL,
  theater_id TEXT NOT NULL,
  failed INTEGER NOT NULL,
  error_summary TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(provider, theater_id)
);
```

```go
type PollAlertState struct {
	Provider domain.ProviderID
	TheaterID string
	Failed bool
	ErrorSummary string
	UpdatedAt time.Time
}

func (r *Repository) GetPollAlertState(context.Context, domain.ProviderID, string) (PollAlertState, error)
func (r *Repository) SavePollAlertState(context.Context, PollAlertState) error
```

- [ ] **Step 4: Run the focused test to verify GREEN**

Run: `go test ./internal/database -run TestPollAlertStatePersistsAcrossReopen -v`

Expected: PASS.

### Task 2: Send deduplicated control-channel transition messages

**Files:**
- Create: `internal/pollalert/service.go`
- Create: `internal/pollalert/service_test.go`
- Modify: `internal/discordbot/notifier.go`
- Modify: `internal/discordbot/notifier_test.go`

- [ ] **Step 1: Write failing service tests**

```go
func TestReportSendsFailureOnceAndRecoveryOnce(t *testing.T) {
	// failure, same failure, success, success
	// assert message texts are ["⚠️ CGV · 용산아이파크몰 조회 실패", "✅ CGV · 용산아이파크몰 조회가 정상화되었습니다"].
}

func TestReportRetriesTransitionAfterDiscordFailure(t *testing.T) {
	// Make the first send fail, then succeed; assert the second failure attempt sends again.
}
```

- [ ] **Step 2: Run the focused test to verify RED**

Run: `go test ./internal/pollalert -v`

Expected: package and `Service.Report` are undefined.

- [ ] **Step 3: Implement reporter and Discord plain-message sender**

```go
type Reporter interface {
	Report(context.Context, database.PollingGroup, string, error) error
}

func (s *Service) Report(ctx context.Context, group database.PollingGroup, theaterName string, fetchErr error) error
```

`Report` reads the installation and current alert state, returns without sending when no transition exists, calls `SendControlMessage` for a transition, and saves the state only after the send succeeds. The failure message truncates the error summary to 500 UTF-8 bytes.

- [ ] **Step 4: Run focused reporter and Discord tests to verify GREEN**

Run: `go test ./internal/pollalert ./internal/discordbot -v`

Expected: PASS.

### Task 3: Report fetch outcomes from scheduled polling

**Files:**
- Modify: `internal/scheduler/scheduler.go`
- Modify: `internal/scheduler/scheduler_test.go`
- Modify: `internal/app/app.go`

- [ ] **Step 1: Write failing scheduler tests**

```go
func TestRunOnceReportsProviderFetchFailure(t *testing.T) {
	// Configure a failing CGV fake provider and recording reporter.
	// Assert one Report call with theater "0013" and the upstream error.
}

func TestRunOnceReportsProviderRecovery(t *testing.T) {
	// Configure a successful provider and assert Report receives nil error.
}
```

- [ ] **Step 2: Run the focused test to verify RED**

Run: `go test ./internal/scheduler -run 'TestRunOnceReportsProvider' -v`

Expected: scheduler options do not accept a reporter and no report is recorded.

- [ ] **Step 3: Inject the reporter and call it only for fetch outcomes**

Add `Reporter` to scheduler options. In `pollBranchWithFetch`, call it after the provider fetch and before snapshot storage; pass `fetchErr` exactly, so SQLite snapshot errors do not trigger provider-failure messages. Use the same call path for prepared CGV fetches and preparation failures. Wire `pollalert.Service` in `app.New` with the repository and Discord notifier.

- [ ] **Step 4: Run scheduler and app construction tests to verify GREEN**

Run: `go test ./internal/scheduler ./internal/app -v`

Expected: PASS.

### Task 4: Verify and publish

**Files:**
- Modify: `README.md`
- Verify all changed Go files

- [ ] **Step 1: Document control-channel error and recovery alerts**

Add one README sentence: provider fetch failures are posted once to `제어`, repeated failures are suppressed, and a later successful fetch posts one recovery message.

- [ ] **Step 2: Format and test**

Run: `gofmt -w internal/database/repository.go internal/database/repository_test.go internal/pollalert/service.go internal/pollalert/service_test.go internal/discordbot/notifier.go internal/discordbot/notifier_test.go internal/scheduler/scheduler.go internal/scheduler/scheduler_test.go internal/app/app.go`

Run: `go test ./... && go vet ./...`

Expected: all packages pass with no vet findings.

- [ ] **Step 3: Commit and push**

```bash
git add README.md internal/database internal/pollalert internal/discordbot internal/scheduler internal/app docs/superpowers
git commit -m "feat: notify control channel of poll errors"
git push origin main
```
