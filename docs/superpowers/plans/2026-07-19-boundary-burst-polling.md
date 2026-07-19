# Boundary Burst Polling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Poll CGV and Megabox seven times from each five-minute boundary through 30 seconds, reusing one prewarmed CGV tab and delivering newly discovered showtimes immediately after every branch result.

**Architecture:** Add a small prepared-poll contract to the domain layer so CGV can expose a reusable Lightpanda session without coupling providers to the scheduler. Replace the scheduler's boundary ticker and random jitter with an explicit prewarm/burst loop that runs at offsets 0, 5, 10, 15, 20, 25, and 30 seconds, skips elapsed offsets, prevents overlapping attempts, and drains completed branch results into serialized Discord delivery calls. Megabox keeps its HTTP transport and participates in every burst offset without a preparation step.

**Tech Stack:** Go 1.26, gorilla/websocket CDP client, SQLite repository, Discord delivery service, Go `testing`

---

### Task 1: Define the reusable prepared-branch contract

**Files:**
- Create: `internal/domain/prepared_poll.go`
- Test: `internal/domain/prepared_poll_test.go`

- [ ] **Step 1: Write the compile-time contract test**

```go
package domain

import "context"

type testPreparedPoll struct{}

func (testPreparedPoll) Fetch(context.Context) ([]Showtime, error) { return nil, nil }
func (testPreparedPoll) Close() error                              { return nil }

var _ PreparedBranchPoll = testPreparedPoll{}
```

- [ ] **Step 2: Run the focused test and verify RED**

Run: `go test ./internal/domain`

Expected: compilation fails because `PreparedBranchPoll` is undefined.

- [ ] **Step 3: Add the minimal interfaces**

```go
package domain

import "context"

type PreparedBranchPoll interface {
	Fetch(context.Context) ([]Showtime, error)
	Close() error
}

type BranchPreparer interface {
	PrepareBranch(context.Context, Branch) (PreparedBranchPoll, error)
}
```

- [ ] **Step 4: Run the focused test and verify GREEN**

Run: `go test ./internal/domain`

Expected: PASS.

- [ ] **Step 5: Commit the contract**

```bash
git add internal/domain/prepared_poll.go internal/domain/prepared_poll_test.go
git commit -m "feat: define prepared branch polling contract"
```

### Task 2: Reuse one Lightpanda target for a complete CGV snapshot

**Files:**
- Modify: `internal/providers/cgv/transport.go`
- Modify: `internal/providers/cgv/provider.go`
- Modify: `internal/providers/cgv/provider_test.go`
- Create: `internal/providers/cgv/transport_test.go`

- [ ] **Step 1: Write failing provider tests for prepared poll reuse and terminal failure**

Add a `fakeSessionOpener` whose `open` method returns one `fakePreparedTransport`. Assert that `PrepareBranch` opens exactly once, two `Fetch` calls use the same transport, `Close` closes it once, and a failed first `Fetch` is returned without issuing transport calls during the second `Fetch`.

```go
func TestPreparedBranchReusesTransportAndClosesIt(t *testing.T) {
	session := &fakePreparedTransport{fakeTransport: fakeTransport{
		dateValues: []string{"20260719"},
		showtimeValues: []showtimeResponse{{SiteNo: "0013", MovNo: "m1", MovNm: "호프", TcscnsGradCd: "03", ScnYmd: "20260719", ScnsNo: "001", ScnSseq: "1", ScnsrtTm: "1200"}},
	}}
	provider := newProvider(&fakeSessionOpener{session: session}, time.Now)
	poll, err := provider.PrepareBranch(context.Background(), domain.Branch{Provider: domain.ProviderCGV, TheaterID: "0013"})
	if err != nil { t.Fatal(err) }
	if _, err := poll.Fetch(context.Background()); err != nil { t.Fatal(err) }
	if _, err := poll.Fetch(context.Background()); err != nil { t.Fatal(err) }
	if err := poll.Close(); err != nil { t.Fatal(err) }
	if session.dateCalls != 2 || session.closeCalls != 1 { t.Fatalf("session=%+v", session) }
}
```

- [ ] **Step 2: Run the CGV tests and verify RED**

Run: `go test ./internal/providers/cgv`

Expected: compilation fails because `PrepareBranch`, `fakeSessionOpener`, and the prepared transport lifecycle do not exist.

- [ ] **Step 3: Extract a reusable CDP session**

Refactor `cdpTransport.get` into `cdpTransport.open(context.Context) (preparedTransport, error)` and `cdpSession.get(context.Context, string, any) error`. The opened session owns the websocket, CDP client, target ID, and session ID. Its idempotent `Close` sends `Target.closeTarget` with a five-second context and then closes the websocket.

```go
type preparedTransport interface {
	transport
	Close() error
}

type sessionOpener interface {
	open(context.Context) (preparedTransport, error)
}

type cdpSession struct {
	connection *websocket.Conn
	client     *cdpClient
	targetID   string
	sessionID  string
	closeOnce  sync.Once
	closeErr   error
}
```

Keep `cdpTransport.dates` and `showtimes` as one-shot compatibility methods, but implement each by opening a session, delegating to it, and closing it. Move the current `Runtime.evaluate` request logic to `cdpSession.get` so repeated calls do not navigate again.

- [ ] **Step 4: Add CGV prepared polling and share parsing code**

Extract `Provider.fetchWithTransport(ctx, branch, transport)` from `FetchBranchSnapshot`. Make `FetchBranchSnapshot` open one reusable transport when its configured transport implements `sessionOpener`; otherwise retain fixture behavior. Implement `PrepareBranch` and a private poll object:

```go
type preparedBranchPoll struct {
	provider    *Provider
	branch      domain.Branch
	transport   preparedTransport
	terminalErr error
	closed      bool
	mu          sync.Mutex
}

func (p *Provider) PrepareBranch(ctx context.Context, branch domain.Branch) (domain.PreparedBranchPoll, error)
func (p *preparedBranchPoll) Fetch(ctx context.Context) ([]domain.Showtime, error)
func (p *preparedBranchPoll) Close() error
```

`Fetch` serializes access, returns `terminalErr` without another CDP request after the first failure, and rejects use after close. Add compile-time assertions that `*Provider` implements `domain.BranchPreparer` and the private poll implements `domain.PreparedBranchPoll`.

- [ ] **Step 5: Run CGV tests and verify GREEN**

Run: `go test ./internal/providers/cgv`

Expected: PASS, including existing response parsing tests.

- [ ] **Step 6: Commit CGV session reuse**

```bash
git add internal/providers/cgv/transport.go internal/providers/cgv/transport_test.go internal/providers/cgv/provider.go internal/providers/cgv/provider_test.go
git commit -m "feat: reuse prepared CGV browser sessions"
```

### Task 3: Generate boundary burst offsets and select the active boundary

**Files:**
- Modify: `internal/scheduler/scheduler.go`
- Modify: `internal/scheduler/scheduler_test.go`

- [ ] **Step 1: Write failing pure timing tests**

```go
func TestBurstOffsetsCoverBoundaryThroughThirtySeconds(t *testing.T) {
	want := []time.Duration{0, 5*time.Second, 10*time.Second, 15*time.Second, 20*time.Second, 25*time.Second, 30*time.Second}
	if got := burstOffsets(); !slices.Equal(got, want) { t.Fatalf("offsets=%v", got) }
}

func TestBurstBoundaryUsesCurrentWindowUntilThirtySeconds(t *testing.T) {
	boundary := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	if got := burstBoundary(boundary.Add(10*time.Second), 5*time.Minute, 30*time.Second); !got.Equal(boundary) { t.Fatalf("boundary=%s", got) }
	if got := burstBoundary(boundary.Add(31*time.Second), 5*time.Minute, 30*time.Second); !got.Equal(boundary.Add(5*time.Minute)) { t.Fatalf("boundary=%s", got) }
}
```

- [ ] **Step 2: Run scheduler tests and verify RED**

Run: `go test ./internal/scheduler`

Expected: compilation fails because `burstOffsets` and `burstBoundary` are undefined.

- [ ] **Step 3: Add fixed timing helpers and remove jitter**

Add fixed constants for ten-second prewarm, five-second spacing, and thirty-second window. Implement the two pure helpers. Remove `Options.Jitter`, the scheduler jitter field, `JitterFrom`, the `math/rand/v2` import, and all jitter sleeps from `pollBranch`. Preserve `RunOnce` as an immediate single snapshot for control and diagnostic paths.

```go
const (
	prewarmLead = 10 * time.Second
	burstStep   = 5 * time.Second
	burstWindow = 30 * time.Second
)

func burstOffsets() []time.Duration {
	offsets := make([]time.Duration, 0, int(burstWindow/burstStep)+1)
	for offset := time.Duration(0); offset <= burstWindow; offset += burstStep {
		offsets = append(offsets, offset)
	}
	return offsets
}
```

- [ ] **Step 4: Run scheduler tests and verify GREEN**

Run: `go test ./internal/scheduler`

Expected: PASS after existing tests stop supplying `Options.Jitter`.

- [ ] **Step 5: Commit deterministic timing helpers**

```bash
git add internal/scheduler/scheduler.go internal/scheduler/scheduler_test.go
git commit -m "feat: define deterministic boundary burst timing"
```

### Task 4: Execute seven polls with CGV prewarming and immediate delivery

**Files:**
- Modify: `internal/scheduler/scheduler.go`
- Modify: `internal/scheduler/scheduler_test.go`

- [ ] **Step 1: Write a failing scheduled-burst test**

Create a controllable fake clock where `Sleep` advances `Now` and records durations. Use a fake CGV provider that implements both `BranchProvider` and `domain.BranchPreparer`, plus a normal Megabox provider. Assert one CGV preparation, seven prepared fetches, seven Megabox fetches per enabled branch, one close, and delivery after each completed branch result.

```go
func TestRunBurstPreparesCGVAndPollsSevenTimes(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 7, 19, 11, 59, 50, 0, time.UTC))
	cgv := &fakePreparingProvider{id: domain.ProviderCGV}
	megabox := &fakeProvider{id: domain.ProviderMegabox}
	store := &fakeStore{states: []database.TargetState{
		{TargetID: "cgv-yongsan-imax", Enabled: true},
		{TargetID: "megabox-coex-dolby", Enabled: true},
	}}
	delivery := &fakeDelivery{}
	scheduler := New(store, delivery, map[domain.ProviderID]BranchProvider{domain.ProviderCGV: cgv, domain.ProviderMegabox: megabox}, Options{Sleep: clock.Sleep, Now: clock.Now})
	if err := scheduler.runBurst(context.Background(), time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)); err != nil { t.Fatal(err) }
	if cgv.prepareCalls != 1 || cgv.poll.fetchCalls != 7 || cgv.poll.closeCalls != 1 { t.Fatalf("cgv=%+v", cgv) }
	if megabox.calls != 7 { t.Fatalf("megabox calls=%d", megabox.calls) }
}
```

- [ ] **Step 2: Run the focused test and verify RED**

Run: `go test ./internal/scheduler -run TestRunBurstPreparesCGVAndPollsSevenTimes -v`

Expected: compilation fails because `runBurst` and the preparation fakes do not exist.

- [ ] **Step 3: Implement resource preparation and offset execution**

Add `preparedGroup` to pair a branch group with either a prepared poll or its normal provider. `runBurst` loads enabled states once, prepares all providers implementing `domain.BranchPreparer`, defers closing every successful preparation, skips elapsed offsets, sleeps until future offsets, and executes each remaining offset sequentially.

```go
type preparedGroup struct {
	group    branchGroup
	provider BranchProvider
	poll     domain.PreparedBranchPoll
}

func (s *Scheduler) runBurst(ctx context.Context, boundary time.Time) error
func (s *Scheduler) prepareGroups(ctx context.Context, groups []branchGroup) ([]preparedGroup, error)
func (s *Scheduler) runBurstAttempt(ctx context.Context, groups []preparedGroup) error
```

Preparation errors are recorded as failed poll runs for the relevant branch and do not prevent normal providers from running. A failed prepared group remains unavailable for the rest of that burst.

- [ ] **Step 4: Deliver after each completed branch without concurrent delivery calls**

In `runBurstAttempt`, launch one poll goroutine per available branch and send its error on a buffered completion channel. Consume one completion at a time and call `DeliverPending` after every branch completion. This allows a fast branch's newly recorded showtimes to be delivered while another branch is still running, without concurrent calls to the delivery service.

```go
completed := make(chan error, len(groups))
for _, group := range groups {
	go func(group preparedGroup) { completed <- s.pollPreparedBranch(ctx, group) }(group)
}
for range groups {
	cycleErr = errors.Join(cycleErr, <-completed)
	cycleErr = errors.Join(cycleErr, s.delivery.DeliverPending(ctx))
}
```

- [ ] **Step 5: Test skipped elapsed offsets, failure isolation, and cancellation cleanup**

Add separate tests that start at boundary plus 12 seconds and assert only offsets 15 through 30 run; make CGV preparation fail and assert Megabox still runs seven times; cancel during a sleep and assert the prepared poll closes exactly once. Add a blocking Megabox fake and assert CGV completion triggers delivery before the Megabox release channel is closed.

- [ ] **Step 6: Run scheduler tests and verify GREEN**

Run: `go test ./internal/scheduler -v`

Expected: PASS with deterministic fake-clock execution and no wall-clock waiting.

- [ ] **Step 7: Commit burst execution**

```bash
git add internal/scheduler/scheduler.go internal/scheduler/scheduler_test.go
git commit -m "feat: poll booking providers through boundary bursts"
```

### Task 5: Replace the ticker loop and document runtime behavior

**Files:**
- Modify: `internal/scheduler/scheduler.go`
- Modify: `internal/scheduler/scheduler_test.go`
- Modify: `README.md`

- [ ] **Step 1: Write a failing Start-loop test**

Use a canceling fake sleep that records the first requested duration. Start at 11:58:00 and assert the scheduler first sleeps 110 seconds to 11:59:50, then prepares the burst. Add a second case starting at 12:00:10 and assert it does not wait for 12:04:50 before joining the current boundary window.

- [ ] **Step 2: Run the focused tests and verify RED**

Run: `go test ./internal/scheduler -run 'TestStart' -v`

Expected: the existing ticker implementation does not prewarm or join the current 30-second window, so assertions fail.

- [ ] **Step 3: Replace `Start` with an explicit boundary loop**

Each iteration computes `burstBoundary(s.now(), s.interval, burstWindow)`, sleeps until `boundary-prewarmLead` when necessary, calls `runBurst`, logs non-cancellation errors, then recomputes from the current wall clock. Do not use `time.Ticker`; recomputation prevents drift after slow attempts.

- [ ] **Step 4: Update operational documentation**

Replace the README description of random 0-10 second jitter with the fixed schedule: CGV prewarms ten seconds before each five-minute boundary; CGV and Megabox poll seven times from `:00` through `:30`; new sessions are delivered after each result; CGV closes its tab after the burst. State that `POLL_INTERVAL_SECONDS` defines the boundary interval while the ten-second prewarm and thirty-second burst are fixed.

- [ ] **Step 5: Run scheduler and full package tests**

Run: `go test ./internal/scheduler ./internal/providers/cgv ./internal/database ./internal/notification`

Expected: PASS.

- [ ] **Step 6: Commit loop and documentation**

```bash
git add internal/scheduler/scheduler.go internal/scheduler/scheduler_test.go README.md
git commit -m "docs: explain boundary burst polling"
```

### Task 6: Final verification and review

**Files:**
- Verify all modified Go and Markdown files

- [ ] **Step 1: Format Go code**

Run: `gofmt -w internal/domain/prepared_poll.go internal/domain/prepared_poll_test.go internal/providers/cgv/transport.go internal/providers/cgv/transport_test.go internal/providers/cgv/provider.go internal/providers/cgv/provider_test.go internal/scheduler/scheduler.go internal/scheduler/scheduler_test.go`

Expected: command exits successfully.

- [ ] **Step 2: Run all automated tests**

Run: `go test ./...`

Expected: PASS for every package.

- [ ] **Step 3: Run race detection and static analysis**

Run: `go test -race ./internal/scheduler ./internal/providers/cgv ./internal/database ./internal/notification`

Expected: PASS without race reports.

Run: `go vet ./...`

Expected: no findings.

- [ ] **Step 4: Check the final diff**

Run: `git diff --check && git status --short`

Expected: no whitespace errors; only intentional implementation and documentation files remain changed.

- [ ] **Step 5: Request code review and address findings**

Use the `requesting-code-review` skill against the full change from `abc0e7c` through the implementation HEAD. Fix any correctness, resource-lifecycle, timing, or concurrency findings and repeat the relevant tests.

- [ ] **Step 6: Commit final review fixes if needed**

```bash
git add internal/domain internal/providers/cgv internal/scheduler README.md
git commit -m "fix: harden boundary burst polling"
```
