# Movie Booking Alert Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go service that lets users in one Discord server subscribe to a CGV or Megabox movie/theater pair and receive a deduplicated DM when new showtimes appear.

**Architecture:** Run one Go process containing the Discord client, provider adapters, polling scheduler, notification service, SQLite persistence, and health server. Provider responses are decoded into dedicated structs and normalized into common domain types; delivery state is stored per subscription/showtime pair so a new subscriber's baseline cannot suppress an existing subscriber's alert.

**Tech Stack:** Go 1.26.5, discordgo, `database/sql`, modernc SQLite, `log/slog`, `net/http`, Go `testing`, Docker

---

## External dependency gate

Implement and verify the platform with deterministic fake Providers first, then add Megabox. CGV support has a mandatory contract gate: an unauthenticated official catalog and showtime request must work through plain Go `net/http` from the production container. Do not bypass access controls, store challenge cookies, add CAPTCHA handling, rotate proxies, or ship a browser inside the service. If the public request cannot be reproduced, keep CGV disabled and report that state accurately while completing the rest of the service.

## File map

```text
.
├── .dockerignore
├── .env.example
├── .gitignore
├── Dockerfile
├── Makefile
├── README.md
├── go.mod
├── go.sum
├── cmd/
│   ├── my-movie/main.go
│   ├── provider-smoke/main.go
│   └── register-commands/main.go
├── docs/provider-contracts/cgv.md
├── internal/
│   ├── app/app.go
│   ├── cache/cache.go
│   ├── config/config.go
│   ├── database/
│   │   ├── database.go
│   │   ├── migrations.go
│   │   ├── repository.go
│   │   └── migrations/001_initial.sql
│   ├── discordbot/
│   │   ├── autocomplete.go
│   │   ├── bot.go
│   │   ├── commands.go
│   │   └── notifier.go
│   ├── domain/
│   │   ├── provider.go
│   │   ├── showtime_key.go
│   │   └── types.go
│   ├── health/server.go
│   ├── httpx/client.go
│   ├── logx/logger.go
│   ├── notification/service.go
│   ├── providers/
│   │   ├── registry.go
│   │   ├── cgv/{provider.go,response.go,transport.go}
│   │   └── megabox/{provider.go,response.go,transport.go}
│   ├── scheduler/scheduler.go
│   └── subscription/service.go
└── testdata/
    ├── cgv/{catalog.json,showtimes.json}
    └── megabox/{bootstrap.json,selected_schedule.json}
```

Tests live beside their packages as `*_test.go`; cross-package flow tests live in `internal/app/app_test.go`.

### Task 1: Scaffold the Go module, configuration, and logging

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `.env.example`
- Create: `Makefile`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `internal/logx/logger.go`

- [ ] **Step 1: Initialize the module and dependencies**

```bash
go mod init my-movie
go get github.com/bwmarrin/discordgo
go get modernc.org/sqlite
```

Set the `go` directive to `1.26.0`. Expected: `go.mod` and `go.sum` exist and `go mod tidy` succeeds.

- [ ] **Step 2: Write failing configuration tests**

```go
func TestLoadUsesOperationalDefaults(t *testing.T) {
	t.Setenv("DISCORD_BOT_TOKEN", "token")
	t.Setenv("DISCORD_APPLICATION_ID", "123")
	t.Setenv("DISCORD_GUILD_ID", "456")

	cfg, err := Load()
	if err != nil { t.Fatal(err) }
	if cfg.DatabasePath != "/data/my-movie.sqlite" { t.Fatalf("path=%q", cfg.DatabasePath) }
	if cfg.PollInterval != 5*time.Minute { t.Fatalf("interval=%s", cfg.PollInterval) }
	if cfg.Port != 3000 { t.Fatalf("port=%d", cfg.Port) }
}

func TestLoadRejectsMissingDiscordToken(t *testing.T) {
	t.Setenv("DISCORD_BOT_TOKEN", "")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "DISCORD_BOT_TOKEN") {
		t.Fatalf("err=%v", err)
	}
}
```

- [ ] **Step 3: Run the tests and confirm the red state**

Run: `go test ./internal/config`

Expected: FAIL because `Load` does not exist.

- [ ] **Step 4: Implement configuration and structured logging**

Define:

```go
type Config struct {
	DiscordBotToken      string
	DiscordApplicationID string
	DiscordGuildID       string
	DatabasePath         string
	PollInterval         time.Duration
	Port                 int
	Timezone             string
}
```

Read the three required Discord variables; default `DATABASE_PATH=/data/my-movie.sqlite`, `POLL_INTERVAL_SECONDS=300`, `PORT=3000`, and `TZ=Asia/Seoul`. Reject non-positive intervals and invalid ports. Build the logger with `slog.NewJSONHandler` and never attach the token to a log record.

- [ ] **Step 5: Add repeatable developer commands**

```makefile
.PHONY: test check run
test:
	go test ./...
check:
	gofmt -w $$(find . -name '*.go')
	go vet ./...
	go test -race ./...
run:
	go run ./cmd/my-movie
```

- [ ] **Step 6: Verify and commit**

```bash
go test ./internal/config
go vet ./...
git add go.mod go.sum .gitignore .env.example Makefile internal/config internal/logx
git commit -m "chore: scaffold go service"
```

Expected: tests and vet PASS.

### Task 2: Define domain contracts, showtime identity, cache, and HTTP policy

**Files:**
- Create: `internal/domain/types.go`
- Create: `internal/domain/provider.go`
- Create: `internal/domain/showtime_key.go`
- Create: `internal/domain/showtime_key_test.go`
- Create: `internal/cache/cache.go`
- Create: `internal/cache/cache_test.go`
- Create: `internal/httpx/client.go`
- Create: `internal/httpx/client_test.go`

- [ ] **Step 1: Write deterministic showtime-key tests**

```go
func TestShowtimeKeyPrefersExternalID(t *testing.T) {
	s := Showtime{Provider: ProviderMegabox, ExternalID: "schedule-1"}
	if got := ShowtimeKey(s); got != "megabox:schedule-1" { t.Fatalf("key=%q", got) }
}

func TestShowtimeFallbackKeyNormalizesAuditorium(t *testing.T) {
	a := Showtime{Provider: ProviderCGV, TheaterID: "0013", MovieID: "m1", PlayDate: "2026-07-19", StartsAt: "09:30", Auditorium: " IMAX관 "}
	b := a
	b.Auditorium = "imax관"
	if ShowtimeKey(a) != ShowtimeKey(b) { t.Fatal("keys differ") }
}
```

- [ ] **Step 2: Write cache and retry tests**

Use an injected clock to prove cached catalog entries expire after 10 minutes. Use `httptest.Server` to prove the client retries 500 responses after 250 ms and 500 ms, applies a 10-second context timeout, honors `Retry-After` for 429, and does not retry a decode/validation error.

- [ ] **Step 3: Run tests and confirm failure**

Run: `go test ./internal/domain ./internal/cache ./internal/httpx`

Expected: FAIL because implementations are missing.

- [ ] **Step 4: Implement common types and the Provider interface**

```go
type ProviderID string
const (
	ProviderCGV ProviderID = "cgv"
	ProviderMegabox ProviderID = "megabox"
)
type Movie struct { ID, Name string }
type Theater struct { ID, Name, AreaCode string }
type Showtime struct {
	Provider ProviderID
	TheaterID, MovieID, ExternalID string
	PlayDate, StartsAt, Auditorium string
}
type BookingLinks struct { App, Web string }

type TheaterProvider interface {
	ID() ProviderID
	SearchMovies(context.Context, string) ([]Movie, error)
	SearchTheaters(context.Context, string) ([]Theater, error)
	FetchShowtimes(context.Context, string, string) ([]Showtime, error)
	BuildBookingLinks(string, string) BookingLinks
}
```

Use SHA-256 over normalized identity fields when `ExternalID` is empty. Implement a generic synchronized cache with a 10-minute TTL and a request client with the tested policy.

- [ ] **Step 5: Verify and commit**

```bash
gofmt -w internal/domain internal/cache internal/httpx
go test ./internal/domain ./internal/cache ./internal/httpx
go vet ./...
git add internal/domain internal/cache internal/httpx
git commit -m "feat: add provider domain contracts"
```

### Task 3: Create SQLite migrations and repositories

**Files:**
- Create: `internal/database/migrations/001_initial.sql`
- Create: `internal/database/migrations.go`
- Create: `internal/database/database.go`
- Create: `internal/database/repository.go`
- Create: `internal/database/repository_test.go`

- [ ] **Step 1: Write repository behavior tests**

Use `t.TempDir()` for a real SQLite file. Test duplicate subscription rejection, close/reopen persistence, and the registration race:

```go
func TestRecordScanKeepsBaselinePerSubscription(t *testing.T) {
	repo := newTestRepository(t)
	oldSub := createSubscription(t, repo, "old-user", StatusActive)
	newSub := createSubscription(t, repo, "new-user", StatusInitializing)

	err := repo.RecordScan(context.Background(), []domain.Showtime{sampleShowtime("s1")}, newSub.ID)
	if err != nil { t.Fatal(err) }
	assertDeliveryStatus(t, repo, oldSub.ID, "megabox:s1", DeliveryPending)
	assertDeliveryStatus(t, repo, newSub.ID, "megabox:s1", DeliveryBaseline)
}
```

Also mark a delivery `sent`, record the same scan again, and assert it remains `sent`.

- [ ] **Step 2: Run the tests and confirm failure**

Run: `go test ./internal/database`

Expected: FAIL because database code does not exist.

- [ ] **Step 3: Add the embedded migration**

Create `subscriptions`, `showtimes`, `notification_deliveries`, and `poll_runs`. Subscription states are `initializing`, `active`, `disabled`; delivery states are `baseline`, `pending`, `sent`, `failed`. Add unique constraints on `(discord_user_id, provider, theater_id, movie_id)` and `(subscription_id, showtime_key)`. Enable foreign keys and WAL.

Use `//go:embed migrations/*.sql` and execute unapplied migrations inside transactions.

- [ ] **Step 4: Implement repository methods**

Implement typed methods for creating/activating/deleting/disabling subscriptions, listing a user's subscriptions, listing active polling groups, recording scans, listing pending delivery groups, marking sent/failed attempts, and recording poll runs. All methods accept `context.Context`; multi-table state changes use `sql.Tx`.

`RecordScan(showtimes, baselineSubscriptionID)` must upsert each showtime, create `baseline` only for the supplied initializing subscription, and create `pending` for every other active matching subscription without overwriting an existing delivery row.

- [ ] **Step 5: Verify and commit**

```bash
gofmt -w internal/database
go test ./internal/database
go vet ./...
git add internal/database
git commit -m "feat: persist subscriptions and deliveries"
```

### Task 4: Implement subscription registration and notifications

**Files:**
- Create: `internal/subscription/service.go`
- Create: `internal/subscription/service_test.go`
- Create: `internal/notification/service.go`
- Create: `internal/notification/service_test.go`

- [ ] **Step 1: Write registration flow tests with fakes**

Test this exact sequence: create initializing row, fetch baseline, persist per-subscription baseline, send confirmation DM, then activate. If fetching or confirmation fails, assert the initializing subscription and its baseline delivery rows are deleted. Assert duplicate registration returns `ErrAlreadySubscribed`.

The race regression must keep an older subscriber's delivery `pending` while the new subscriber receives `baseline` for the same showtime.

- [ ] **Step 2: Write notification state tests**

Use a fake Notifier and fake clock. Assert:

- showtimes are grouped by user/provider/theater/movie/date and sorted by start time;
- one successful send marks every included row `sent`;
- transient failures stop after three total attempts and become `failed`;
- Discord DM rejection disables the subscription;
- app and web buttons both contain HTTPS URLs.

- [ ] **Step 3: Run tests and confirm failure**

Run: `go test ./internal/subscription ./internal/notification`

Expected: FAIL because services do not exist.

- [ ] **Step 4: Implement registration with cleanup**

```go
sub, err := repo.CreateInitializingSubscription(ctx, input)
if err != nil { return Subscription{}, err }
cleanup := true
defer func() { if cleanup { _ = repo.DeleteSubscription(context.Background(), sub.ID) } }()

showtimes, err := provider.FetchShowtimes(ctx, input.Movie.ID, input.Theater.ID)
if err != nil { return Subscription{}, err }
if err := repo.RecordScan(ctx, showtimes, sub.ID); err != nil { return Subscription{}, err }
if err := notifier.SendRegistrationConfirmation(ctx, input.DiscordUserID, sub); err != nil { return Subscription{}, err }
if err := repo.ActivateSubscription(ctx, sub.ID); err != nil { return Subscription{}, err }
cleanup = false
return sub, nil
```

- [ ] **Step 5: Implement notification delivery**

Build one Discord message per grouping with movie, theater, play date, ordered times, and two link buttons. Classify retryable errors separately from permanent DM-disabled errors. Update attempts transactionally after each send result.

- [ ] **Step 6: Verify and commit**

```bash
gofmt -w internal/subscription internal/notification
go test ./internal/subscription ./internal/notification
go test -race ./internal/subscription ./internal/notification
git add internal/subscription internal/notification
git commit -m "feat: register subscriptions and deliver alerts"
```

### Task 5: Implement the Megabox Provider

**Files:**
- Create: `internal/providers/megabox/response.go`
- Create: `internal/providers/megabox/transport.go`
- Create: `internal/providers/megabox/provider.go`
- Create: `internal/providers/megabox/provider_test.go`
- Create: `testdata/megabox/bootstrap.json`
- Create: `testdata/megabox/selected_schedule.json`

- [ ] **Step 1: Save minimal redacted response fixtures**

The bootstrap fixture contains one movie, one theater, and two bookable dates. The selected fixture contains two schedules with distinct schedule IDs and start times. Retain only fields decoded by production structs; save no cookies, headers, or device values.

- [ ] **Step 2: Write fixture-driven Provider tests**

Assert case-insensitive movie/theater search, maximum normalized fields, all bookable dates queried, duplicate schedule IDs removed, unavailable schedules excluded, malformed success responses rejected, and HTTPS booking links containing supported movie/theater IDs.

```go
func TestFetchShowtimesNormalizesSchedule(t *testing.T) {
	p := newFixtureProvider(t)
	got, err := p.FetchShowtimes(context.Background(), "m1", "1372")
	if err != nil { t.Fatal(err) }
	want := domain.Showtime{
		Provider: domain.ProviderMegabox, TheaterID: "1372", MovieID: "m1",
		ExternalID: "schedule-1", PlayDate: "2026-07-19", StartsAt: "14:00", Auditorium: "6관",
	}
	if diff := cmpShowtime(want, got[0]); diff != "" { t.Fatal(diff) }
}
```

- [ ] **Step 3: Run tests and confirm failure**

Run: `go test ./internal/providers/megabox`

Expected: FAIL because the Provider does not exist.

- [ ] **Step 4: Implement the official booking request**

POST JSON to the official simple-booking endpoint using `Content-Type: application/json; charset=UTF-8`, official origin/referer, no stored cookies, and `httpx.Client`. Bootstrap with the current `YYYYMMDD`, `onLoad: "Y"`, and an empty sales channel. For a selected pair send the representative movie ID, one theater ID, its area code, and blank unused selection slots.

Decode and validate these required fields:

```go
type movieResponse struct { MovieNo, MovieNm string }
type theaterResponse struct { BrchNo, BrchNm, AreaCd, BrchFormAt string }
type scheduleResponse struct {
	PlaySchdlNo, BrchNo, MovieNo, RpstMovieNo string
	PlayDe, PlayStartTime, TheabExpoNm, BokdAbleAt string
}
```

Only normalize `BokdAbleAt == "Y"`; convert `YYYYMMDD` to `YYYY-MM-DD`; use the schedule number as `ExternalID`.

- [ ] **Step 5: Build official HTTPS booking links**

The web URL carries the representative movie and branch query parameters. The app button uses the official mobile booking HTTPS URL. Tests assert exact origins, HTTPS schemes, and encoded identifiers where supported.

- [ ] **Step 6: Verify and commit**

```bash
gofmt -w internal/providers/megabox
go test ./internal/providers/megabox
go vet ./...
git add internal/providers/megabox testdata/megabox
git commit -m "feat: add megabox showtime provider"
```

### Task 6: Establish the CGV contract gate

**Files:**
- Create: `docs/provider-contracts/cgv.md`
- Create only when verified: `testdata/cgv/catalog.json`
- Create only when verified: `testdata/cgv/showtimes.json`

- [ ] **Step 1: Capture an unauthenticated official request**

In a signed-out real browser, select a theater and movie and inspect Fetch/XHR traffic. Record method, URL, non-secret headers, body, response content type, and fields required for movie, theater, showtime, date, time, and auditorium identity. Do not record cookies, authorization values, challenge tokens, or device fingerprints.

- [ ] **Step 2: Reproduce it with a temporary Go probe**

Use `http.NewRequestWithContext`, a 10-second client timeout, and only non-secret headers. Run the probe locally and inside `golang:1.26.5-alpine` with no mounted browser profile or cookie file.

Expected when verified: both return a valid catalog/showtime payload. If either is rejected, write `Status: blocked` and the HTTP status/reason category to the contract document, omit CGV from the registry, delete temporary probe code, and continue to Task 8.

- [ ] **Step 3: Save only verified, redacted fixtures**

When verified, minimize fixtures to fields needed by the production response structs. Document reproduction commands and verification date. Do not include third-party project references.

- [ ] **Step 4: Scan and commit the gate result**

```bash
rg -n "Cookie|Authorization|Bearer|cf_clearance|JSESSIONID" docs/provider-contracts/cgv.md testdata/cgv || true
git add docs/provider-contracts/cgv.md testdata/cgv
git commit -m "docs: record cgv provider contract"
```

Expected: no secret values; document says exactly `verified` or `blocked`.

### Task 7: Implement the CGV Provider only when verified

**Files:**
- Create: `internal/providers/cgv/response.go`
- Create: `internal/providers/cgv/transport.go`
- Create: `internal/providers/cgv/provider.go`
- Create: `internal/providers/cgv/provider_test.go`

- [ ] **Step 1: Write fixture-driven contract tests**

Assert movie and theater search, required-field rejection, date/time normalization, auditorium normalization, stable external ID or deterministic fallback key, and official HTTPS app/web links.

The normalized fixture expectation is:

```go
domain.Showtime{
	Provider: domain.ProviderCGV,
	TheaterID: "0013", MovieID: "movie-id", ExternalID: "schedule-id",
	PlayDate: "2026-07-19", StartsAt: "10:20", Auditorium: "IMAX관",
}
```

Omit `ExternalID` only if the verified payload has no stable schedule identifier.

- [ ] **Step 2: Run tests and confirm failure**

Run: `go test ./internal/providers/cgv`

Expected: FAIL because the Provider does not exist.

- [ ] **Step 3: Implement verified response structs and transport**

Map every identity field from the verified contract into typed structs and implement `Validate() error` methods for required values. Use `httpx.Client`; require normal TLS verification; do not read long-lived upstream sessions from environment variables.

- [ ] **Step 4: Normalize data and links**

Return `YYYY-MM-DD`, `HH:mm`, trimmed auditorium text, and a stable external schedule ID when present. Build only official HTTPS links and include movie/theater IDs when supported by the verified contract.

- [ ] **Step 5: Verify and commit**

```bash
gofmt -w internal/providers/cgv
go test ./internal/providers/cgv
go vet ./...
git add internal/providers/cgv
git commit -m "feat: add cgv showtime provider"
```

Skip Task 7 completely when Task 6 is blocked.

### Task 8: Implement Provider registry and Discord commands

**Files:**
- Create: `internal/providers/registry.go`
- Create: `internal/providers/registry_test.go`
- Create: `internal/discordbot/commands.go`
- Create: `internal/discordbot/autocomplete.go`
- Create: `internal/discordbot/notifier.go`
- Create: `internal/discordbot/bot.go`
- Create: `internal/discordbot/bot_test.go`
- Create: `cmd/register-commands/main.go`

- [ ] **Step 1: Write registry and autocomplete tests**

Assert disabled Providers are not advertised, movie choices stop at Discord's 25-item limit, the chosen Provider alone receives theater searches, deletion choices include only the invoking user's subscriptions, and an arbitrary movie/theater ID is rejected before a database write.

- [ ] **Step 2: Run tests and confirm failure**

Run: `go test ./internal/providers ./internal/discordbot`

Expected: FAIL because registry and Discord packages do not exist.

- [ ] **Step 3: Define one guild-scoped `/알림` command**

Create `등록`, `목록`, `삭제`, `전체삭제`, and `도움말` subcommands. Registration requires `영화관`, `지점`, and `영화`; the latter two use autocomplete. Deletion autocompletes the caller's subscriptions. Every command response is ephemeral.

- [ ] **Step 4: Route interactions and validate selections**

Handle application-command autocomplete separately from submitted commands. Before registration, resolve selected IDs through the Provider's latest cached catalog so arbitrary values cannot be persisted. Defer responses for operations that perform network calls.

- [ ] **Step 5: Implement DM notification transport**

Send registration confirmation only after baseline persistence. Build embeds and URL buttons for alerts. Map Discord error code `50007` to a typed permanent DM-disabled error; return other errors for retry classification.

- [ ] **Step 6: Register guild commands deterministically**

The command utility reads configuration, creates a Discord session, replaces the configured guild's command set, prints command names, and exits without starting the scheduler.

- [ ] **Step 7: Verify and commit**

```bash
gofmt -w internal/providers/registry.go internal/discordbot cmd/register-commands
go test ./internal/providers ./internal/discordbot
go vet ./...
git add internal/providers/registry.go internal/providers/registry_test.go internal/discordbot cmd/register-commands
git commit -m "feat: add discord subscription commands"
```

### Task 9: Add polling scheduler and health server

**Files:**
- Create: `internal/scheduler/scheduler.go`
- Create: `internal/scheduler/scheduler_test.go`
- Create: `internal/health/server.go`
- Create: `internal/health/server_test.go`

- [ ] **Step 1: Write scheduler tests**

Use fake Providers and an injected clock/random source. Assert matching subscriptions share one request, each Provider has at most two concurrent requests, jitter stays within 0–10 seconds, cycles never overlap, one Provider failure does not stop another, poll runs are recorded, and pending deliveries run after scans.

- [ ] **Step 2: Write health tests**

Assert 200 with no active subscriptions, 200 when active Providers succeeded within two intervals, 503 when success is absent/stale, and 503 on database ping failure. Assert responses contain no Discord user IDs, subscription details, or upstream payloads.

- [ ] **Step 3: Run tests and confirm failure**

Run: `go test ./internal/scheduler ./internal/health`

Expected: FAIL because packages do not exist.

- [ ] **Step 4: Implement a non-overlapping scheduler**

Use one ticker and an atomic/mutex running guard. Group by Provider/theater/movie. Use a per-Provider semaphore of capacity two and context cancellation for shutdown. Record each group result independently, then call notification delivery once per completed cycle.

- [ ] **Step 5: Implement `/health`**

Serve with `http.Server` and explicit read-header, read, write, and idle timeouts. Return a small JSON body and 503 when any active Provider lacks a successful poll within twice the configured interval.

- [ ] **Step 6: Verify and commit**

```bash
gofmt -w internal/scheduler internal/health
go test ./internal/scheduler ./internal/health
go test -race ./internal/scheduler
git add internal/scheduler internal/health
git commit -m "feat: schedule polling and expose health"
```

### Task 10: Assemble the process and graceful shutdown

**Files:**
- Create: `internal/app/app.go`
- Create: `internal/app/app_test.go`
- Create: `cmd/my-movie/main.go`
- Create: `cmd/provider-smoke/main.go`

- [ ] **Step 1: Write application lifecycle tests**

Assert startup order is database, registry, health, Discord, scheduler. Inject a Discord startup failure and assert health/database close. Call shutdown twice and assert every resource closes once. Cancel during a poll and assert shutdown waits for that poll's context to finish.

- [ ] **Step 2: Run tests and confirm failure**

Run: `go test ./internal/app`

Expected: FAIL because application assembly does not exist.

- [ ] **Step 3: Implement one composition root**

`app.New(config, dependencies)` opens the database, builds catalog cache and verified Provider registry, creates services, and returns an application without starting side effects at import/package-init time.

- [ ] **Step 4: Implement ordered start and idempotent shutdown**

Start migrations, health, Discord, then scheduler. On SIGTERM/SIGINT stop new Discord interactions, stop the ticker, cancel and wait for polling, close Discord, shut down HTTP with timeout, and close SQLite. Protect shutdown with `sync.Once`.

- [ ] **Step 5: Implement Provider smoke CLI and operational subcommands**

`provider-smoke` accepts exactly `cgv` or `megabox`, prints counts and one redacted normalized result, exits 2 for a disabled Provider, and exits 1 for a contract failure. `my-movie healthcheck` calls local `/health` and exits nonzero unless status is 2xx. `my-movie database-check` reads only `DATABASE_PATH`, opens the database, applies migrations, prints the latest migration version, and exits; neither operational subcommand requires Discord environment variables.

- [ ] **Step 6: Verify and commit**

```bash
gofmt -w internal/app cmd
go test ./...
go vet ./...
git add internal/app cmd
git commit -m "feat: assemble movie alert service"
```

### Task 11: Package and document Docker operation

**Files:**
- Create: `Dockerfile`
- Create: `.dockerignore`
- Create: `README.md`
- Modify: `.env.example`

- [ ] **Step 1: Build a static multi-stage image**

Use `golang:1.26.5-alpine` as builder, `CGO_ENABLED=0`, `go mod download`, and `go build -trimpath -ldflags='-s -w'`. Create `/data` in the builder, then use `scratch` as runtime and copy `/data` with ownership `65532:65532` together with CA certificates and the binary. Import `time/tzdata` in the executable, set `USER 65532:65532`, expose 3000, and set:

```dockerfile
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD ["/my-movie", "healthcheck"]
```

- [ ] **Step 2: Document setup and operation**

README covers Discord application creation, token handling, guild command registration, required permissions, DM availability, environment settings, tests, race tests, Docker build/run with `my-movie-data:/data`, interval adjustment, SQLite volume backup, `/health`, Provider status, and smoke commands. Do not cite or describe external example projects.

- [ ] **Step 3: Verify image and persistent volume**

```bash
go test ./...
go test -race ./...
go vet ./...
docker build -t my-movie-alert:test .
```

Run `my-movie database-check` twice in separate containers using the same temporary named volume. Expected: both runs print migration version `1`, the second run reuses the existing SQLite file without an error, and the volume contains a non-empty database. Subscription row persistence remains covered by the repository close/reopen test from Task 3.

- [ ] **Step 4: Commit packaging**

```bash
git add Dockerfile .dockerignore README.md .env.example
git commit -m "docs: add docker operations guide"
```

### Task 12: Final acceptance verification

**Files:**
- Modify only files required by failures found here.

- [ ] **Step 1: Run all offline checks**

```bash
gofmt -w $$(find . -name '*.go')
go test ./...
go test -race ./...
go vet ./...
```

Expected: every command succeeds.

- [ ] **Step 2: Run live Provider smoke checks**

```bash
go run ./cmd/provider-smoke megabox
go run ./cmd/provider-smoke cgv
```

Expected: Megabox succeeds. CGV succeeds only if its gate is verified; otherwise exit 2 and documentation clearly marks it unavailable.

- [ ] **Step 3: Verify the Discord happy path**

Register guild commands, subscribe to one current movie/theater pair, receive the confirmation DM, inspect `/알림 목록`, delete it, and confirm the list is empty. Use the fake Provider integration test to prove a newly inserted showtime creates one embed containing sorted times and both booking buttons without waiting for a real opening.

- [ ] **Step 4: Scan for secrets and prohibited documentation material**

Inspect `README.md`, `docs`, source, fixtures, and tests. Confirm there are no Bot token values, authorization headers, cookies, browser challenge values, or descriptions/links copied from external example projects.

- [ ] **Step 5: Inspect the final repository state**

```bash
git status --short
git diff --check
git log --oneline --decorate -12
```

Expected: no whitespace errors, a clean worktree, and focused commits corresponding to the tasks above.
