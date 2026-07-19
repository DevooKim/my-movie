# Fixed Premium Theater Alerts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restrict registration to five fixed premium-theater targets, add the current CGV JSON provider, and alert only for the selected premium format.

**Architecture:** Add a static `AlertTarget` catalog that owns stable target IDs, real theater IDs, and provider-native auditorium codes. Pass the target through registration, persistence, polling, provider filtering, link generation, and notification formatting; migrate legacy subscriptions to disabled status because they lack a premium-format identity.

**Tech Stack:** Go 1.24, SQLite via `modernc.org/sqlite`, DiscordGo, `net/http`, table-driven Go tests, Docker

---

## File structure

- Create `internal/domain/target.go`: target type and provider interface signatures.
- Create `internal/targets/catalog.go`: the only definition of the five supported targets.
- Create `internal/targets/catalog_test.go`: catalog invariants and exact target values.
- Create `internal/database/migrations/002_fixed_targets.sql`: subscription schema rebuild and legacy disablement.
- Modify `internal/database/{migrations.go,repository.go,repository_test.go}`: persist and query target identity.
- Modify `internal/providers/megabox/{response.go,provider.go,provider_test.go}` and fixtures: filter `DBC` schedules.
- Create `internal/providers/cgv/{response.go,transport.go,provider.go,provider_test.go}` and `testdata/cgv/*.json`: current CGV JSON provider.
- Modify `internal/discordbot/{commands.go,handler.go,bot.go,*_test.go}`: fixed target choice and target-aware movie autocomplete.
- Modify `internal/subscription/service.go`, `internal/scheduler/{scheduler.go,scheduler_test.go}`, and notification code: target-aware polling and messages.
- Modify `internal/app/app.go`, `cmd/provider-smoke/main.go`, and registry tests: enable CGV and wire the target catalog.
- Modify `README.md` and `docs/provider-contracts/cgv.md`: document the verified current behavior and supported targets.

### Task 1: Add the fixed target domain and catalog

**Files:**
- Create: `internal/domain/target.go`
- Modify: `internal/domain/provider.go`
- Create: `internal/targets/catalog.go`
- Create: `internal/targets/catalog_test.go`

- [ ] **Step 1: Write the failing catalog tests**

```go
func TestCatalogContainsOnlySupportedTargets(t *testing.T) {
	got := targets.All()
	want := []domain.AlertTarget{
		{ID: "megabox-coex-dolby", Provider: domain.ProviderMegabox, Theater: domain.Theater{ID: "1351", Name: "코엑스", AreaCode: "10"}, AuditoriumName: "Dolby Cinema", AuditoriumCode: "DBC"},
		{ID: "megabox-namhyeona-dolby", Provider: domain.ProviderMegabox, Theater: domain.Theater{ID: "0019", Name: "남양주현대아울렛 스페이스원", AreaCode: "30"}, AuditoriumName: "Dolby Cinema", AuditoriumCode: "DBC"},
		{ID: "cgv-yongsan-imax", Provider: domain.ProviderCGV, Theater: domain.Theater{ID: "0013", Name: "용산아이파크몰"}, AuditoriumName: "IMAX", AuditoriumCode: "03"},
		{ID: "cgv-yongsan-4dx", Provider: domain.ProviderCGV, Theater: domain.Theater{ID: "0013", Name: "용산아이파크몰"}, AuditoriumName: "4DX", AuditoriumCode: "02"},
		{ID: "cgv-yongsan-screenx", Provider: domain.ProviderCGV, Theater: domain.Theater{ID: "0013", Name: "용산아이파크몰"}, AuditoriumName: "SCREENX", AuditoriumCode: "04"},
	}
	if diff := cmp.Diff(want, got); diff != "" { t.Fatal(diff) }
}

func TestCatalogRejectsUnknownTarget(t *testing.T) {
	if _, ok := targets.Find("forged"); ok { t.Fatal("unexpected target") }
}
```

- [ ] **Step 2: Run the tests and verify RED**

Run: `go test ./internal/targets`

Expected: FAIL because `domain.AlertTarget` and package `internal/targets` do not exist.

- [ ] **Step 3: Implement the target and catalog**

```go
type AlertTarget struct {
	ID             string
	Provider       ProviderID
	Theater        Theater
	AuditoriumName string
	AuditoriumCode string
}

func (t AlertTarget) DisplayName() string {
	provider := string(t.Provider)
	if t.Provider == ProviderMegabox { provider = "메가박스" }
	if t.Provider == ProviderCGV { provider = "CGV" }
	return provider + " " + t.Theater.Name + " · " + t.AuditoriumName
}
```

Define the five immutable values in `internal/targets/catalog.go`. Return cloned slices from `All`, exact values from `Find`, and provide `MustFind` for test/setup code that panics only when a compile-time catalog ID is wrong.

Change the provider contract to:

```go
type TheaterProvider interface {
	ID() ProviderID
	SearchMovies(context.Context, string) ([]Movie, error)
	FetchShowtimes(context.Context, AlertTarget, string) ([]Showtime, error)
	BuildBookingLinks(AlertTarget, string) BookingLinks
}
```

- [ ] **Step 4: Run tests and verify GREEN**

Run: `go test ./internal/targets ./internal/domain`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain internal/targets
git commit -m "feat: define fixed premium theater targets"
```

### Task 2: Migrate subscriptions to target identity

**Files:**
- Create: `internal/database/migrations/002_fixed_targets.sql`
- Modify: `internal/database/migrations.go`
- Modify: `internal/database/repository.go`
- Modify: `internal/database/repository_test.go`

- [ ] **Step 1: Write failing migration and repository tests**

Add a migration test that opens a version-1 database containing one active legacy subscription and delivery, reopens it with current migrations, and asserts:

```go
if got.Status != database.StatusDisabled { t.Fatalf("status=%s", got.Status) }
if got.TargetID != "" { t.Fatalf("target=%q", got.TargetID) }
```

Add a repository test creating three subscriptions for one user, one CGV theater and movie, with target IDs `cgv-yongsan-imax`, `cgv-yongsan-4dx`, and `cgv-yongsan-screenx`; all three inserts must succeed, while a second IMAX insert must fail with the unique constraint.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./internal/database`

Expected: FAIL because target columns and migration 2 do not exist.

- [ ] **Step 3: Implement migration 2**

The SQL migration must:

1. Drop `notification_deliveries`, whose legacy baseline cannot be assigned to a format.
2. Rename `subscriptions` to `subscriptions_legacy`.
3. Create `subscriptions` with `target_id TEXT NOT NULL`, `auditorium_name TEXT NOT NULL`, and `UNIQUE(discord_user_id, provider, target_id, movie_id)`.
4. Copy legacy rows with `target_id=''`, `auditorium_name=''`, and `status='disabled'`.
5. Drop `subscriptions_legacy`.
6. Recreate `notification_deliveries` with its original foreign key and status constraints.
7. Add `target_id TEXT NOT NULL DEFAULT ''` to `poll_runs`.

Generalize `migrate` to apply embedded numbered migrations in order and record each applied version atomically.

- [ ] **Step 4: Make repository types target-aware**

```go
type Subscription struct {
	ID, DiscordUserID, TargetID, AuditoriumName string
	Provider domain.ProviderID
	Theater domain.Theater
	Movie domain.Movie
	Status SubscriptionStatus
	CreatedAt, UpdatedAt time.Time
}

type PollingGroup struct {
	Provider domain.ProviderID
	TargetID string
	MovieID string
}
```

Persist the target fields, select them in every subscription query, include `target_id` in active polling groups and poll runs, and make `matchingSubscriptions` match `provider + target_id + movie_id`. Add `TargetID` to `domain.Showtime` and its deterministic key.

- [ ] **Step 5: Run tests and verify GREEN**

Run: `go test ./internal/database ./internal/domain`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/database internal/domain
git commit -m "feat: persist fixed alert targets"
```

### Task 3: Filter Megabox schedules to Dolby Cinema

**Files:**
- Modify: `internal/providers/megabox/response.go`
- Modify: `internal/providers/megabox/provider.go`
- Modify: `internal/providers/megabox/provider_test.go`
- Modify: `testdata/megabox/selected_schedule.json`

- [ ] **Step 1: Add a failing provider test**

Extend the fixture with one `theabKindCd: "DBC"` schedule and one regular schedule. Assert that fetching `megabox-coex-dolby` returns only the DBC schedule and sets `TargetID`.

```go
got, err := provider.FetchShowtimes(ctx, target, "m1")
if err != nil { t.Fatal(err) }
if len(got) != 1 || got[0].TargetID != target.ID || got[0].Auditorium != "DOLBY CINEMA [Laser]" {
	t.Fatalf("showtimes=%+v", got)
}
```

Also assert that a CGV target passed to the Megabox provider returns a provider-mismatch error.

- [ ] **Step 2: Run test and verify RED**

Run: `go test ./internal/providers/megabox`

Expected: FAIL because the old provider accepts theater IDs and does not decode `theabKindCd`.

- [ ] **Step 3: Implement target-aware filtering**

Add `TheabKindCd string \`json:"theabKindCd"\`` to `scheduleResponse` and require it during validation. Validate the target provider and catalog theater, issue the existing selected requests, then retain only schedules satisfying:

```go
schedule.BokdAbleAt == "Y" &&
	schedule.BrchNo == target.Theater.ID &&
	schedule.RpstMovieNo == movieID &&
	schedule.TheabKindCd == target.AuditoriumCode
```

Set `TargetID: target.ID` on normalized showtimes and update booking links to use `target.Theater.ID`.

- [ ] **Step 4: Run tests and verify GREEN**

Run: `go test ./internal/providers/megabox`

Expected: PASS, including the existing late-night regression test.

- [ ] **Step 5: Commit**

```bash
git add internal/providers/megabox testdata/megabox
git commit -m "feat: restrict megabox alerts to dolby cinema"
```

### Task 4: Implement the current CGV JSON provider

**Files:**
- Create: `internal/providers/cgv/response.go`
- Create: `internal/providers/cgv/transport.go`
- Create: `internal/providers/cgv/provider.go`
- Create: `internal/providers/cgv/provider_test.go`
- Create: `testdata/cgv/movies.json`
- Create: `testdata/cgv/dates.json`
- Create: `testdata/cgv/showtimes.json`

- [ ] **Step 1: Save minimal sanitized fixtures and write failing tests**

Fixtures must contain only fields decoded in production. Include one schedule for each `tcscnsGradCd` value `02`, `03`, `04`, one normal hall, and a CGV late-night value such as `scnsrtTm: "2440"`.

Test movie search, target-code filtering, stable external ID construction from `siteNo + scnYmd + scnsNo + scnSseq + movNo`, malformed required fields, and late-night normalization.

```go
got, err := provider.FetchShowtimes(ctx, targets.MustFind("cgv-yongsan-imax"), "30001297")
if err != nil { t.Fatal(err) }
if len(got) != 1 || got[0].Auditorium != "IMAX관" { t.Fatalf("showtimes=%+v", got) }
if got[0].PlayDate != "2026-07-20" || got[0].StartsAt != "00:40" { t.Fatalf("late=%+v", got[0]) }
```

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./internal/providers/cgv`

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Implement response contracts and transport**

Use these unauthenticated GET routes on `https://cgv.co.kr`:

```text
/api/v1/booking/searchAtktTopPostrList?coCd=A420&movNm={query}&div=&attrCd=
/api/v1/booking/searchSiteScnscYmdListBySite?coCd=A420&siteNo={siteNo}
/api/v1/booking/searchMovScnInfo?coCd=A420&siteNo={siteNo}&scnYmd={YYYYMMDD}&rtctlScopCd=08
```

Send `Accept: application/json`, `Accept-Language: ko-KR`, and the official booking-page Referer through `httpx.Client`. Do not add cookies, authorization, client IDs, challenge state, or browser dependencies.

- [ ] **Step 4: Implement normalization and filtering**

Validate `statusCode == 0`. Search movies case-insensitively. Fetch all advertised dates, deduplicate schedules by the stable external ID, and keep only records whose `siteNo`, `movNo`, and `tcscnsGradCd` match the target and movie.

Normalize four-digit start times with hour range `0..47`, roll the date by `hour/24`, and format `hour%24` as `HH:mm`. Build official HTTPS booking links targeting the CGV booking screen.

- [ ] **Step 5: Run tests and verify GREEN**

Run: `go test ./internal/providers/cgv`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/providers/cgv testdata/cgv
git commit -m "feat: add cgv premium theater provider"
```

### Task 5: Change Discord registration to fixed targets

**Files:**
- Modify: `internal/discordbot/commands.go`
- Modify: `internal/discordbot/handler.go`
- Modify: `internal/discordbot/bot.go`
- Modify: `internal/discordbot/handler_test.go`
- Modify: `internal/discordbot/bot_test.go`
- Modify: `internal/discordbot/notifier.go`
- Modify: `internal/discordbot/notifier_test.go`
- Modify: `internal/subscription/service.go`
- Modify: `internal/subscription/service_test.go`

- [ ] **Step 1: Write failing command and handler tests**

Assert the registration command exposes exactly `상영관` and `영화`, with five static target choices. Assert movie autocomplete resolves the target, calls only that provider, rejects unknown targets, and registration persists the target.

```go
choices, err := handler.MovieChoices(ctx, "cgv-yongsan-imax", "호프")
if err != nil { t.Fatal(err) }
if cgv.searches != 1 || megabox.searches != 0 { t.Fatal("wrong provider") }
```

Assert registration confirmation, list output, delete choices, and alert embeds contain `target.DisplayName()`.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./internal/discordbot ./internal/subscription`

Expected: FAIL because handlers still accept provider and theater separately.

- [ ] **Step 3: Implement fixed-target command flow**

Build five Discord choices from `targets.All()`. Change autocomplete and registration signatures to accept `targetID, movieID`. Resolve the target before provider lookup, validate the movie from that provider, and pass these fields to subscription registration:

```go
database.CreateSubscriptionInput{
	DiscordUserID: userID,
	Provider: target.Provider,
	TargetID: target.ID,
	AuditoriumName: target.AuditoriumName,
	Theater: target.Theater,
	Movie: movie,
}
```

Use stored target and auditorium names in all user-visible registration, listing, deletion, confirmation, and alert strings.

- [ ] **Step 4: Run tests and verify GREEN**

Run: `go test ./internal/discordbot ./internal/subscription`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/discordbot internal/subscription
git commit -m "feat: register alerts by fixed premium target"
```

### Task 6: Make polling and delivery target-aware

**Files:**
- Modify: `internal/scheduler/scheduler.go`
- Modify: `internal/scheduler/scheduler_test.go`
- Modify: `internal/notification/service.go`
- Modify: `internal/notification/service_test.go`

- [ ] **Step 1: Write failing scheduler and delivery tests**

Create three polling groups for one CGV movie using the IMAX, 4DX, and SCREENX target IDs. Assert each group resolves its target and passes it to the provider. Assert a showtime recorded for IMAX creates delivery rows only for IMAX subscribers.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./internal/scheduler ./internal/notification`

Expected: FAIL because polling groups contain theater IDs and providers receive no target.

- [ ] **Step 3: Implement target resolution**

Resolve `group.TargetID` through the fixed catalog before calling:

```go
showtimes, err := provider.FetchShowtimes(ctx, target, group.MovieID)
```

Treat a missing catalog target as a permanent group error recorded in `poll_runs`. Build notification links with the stored target identity and keep delivery grouping isolated by subscription.

- [ ] **Step 4: Run tests and verify GREEN**

Run: `go test ./internal/scheduler ./internal/notification`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/scheduler internal/notification
git commit -m "feat: poll and deliver by premium target"
```

### Task 7: Wire providers, update operations, and verify live contracts

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`
- Modify: `internal/providers/registry_test.go`
- Modify: `cmd/provider-smoke/main.go`
- Modify: `cmd/provider-smoke/main_test.go`
- Modify: `README.md`
- Modify: `docs/provider-contracts/cgv.md`

- [ ] **Step 1: Write failing wiring and smoke tests**

Assert the application registry contains both providers. Change provider smoke to select the first fixed target belonging to the requested provider and print `targetId` in its JSON result. Assert an unknown or provider-mismatched target fails without issuing a request.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./internal/app ./internal/providers ./cmd/provider-smoke`

Expected: FAIL because CGV is not registered and smoke still searches arbitrary theaters.

- [ ] **Step 3: Wire both providers and update operations text**

Construct one shared HTTP client, register `megabox.New(...)` and `cgv.New(...)`, and pass the fixed catalog where handlers or schedulers require target resolution. Update README commands and provider status to describe five fixed targets and both enabled providers. Update the local CGV contract document with the verified unauthenticated endpoints and required non-secret headers only.

- [ ] **Step 4: Run formatting and the complete local suite**

Run:

```bash
gofmt -w cmd internal
go test ./...
go test -race ./...
go vet ./...
git diff --check
```

Expected: every command exits 0 with no warnings.

- [ ] **Step 5: Run live smoke tests**

Run:

```bash
go run ./cmd/provider-smoke megabox
go run ./cmd/provider-smoke cgv
```

Expected: each prints JSON with its provider, a supported target ID, nonzero movie count, and normalized premium-format showtimes when the selected movie currently has one. If the first movie has no premium-format schedule, smoke must scan movies deterministically until it finds a current schedule or report an explicit inconclusive contract result rather than claiming the API failed.

- [ ] **Step 6: Re-register the Discord guild command and rebuild runtime**

Run the existing command registration and Docker build/start workflow using environment values from the local `.env` file without printing them. Verify `/health` returns 200 and the container remains healthy.

- [ ] **Step 7: Rotate the exposed Discord bot token**

Generate a new bot token in Discord Developer Portal, replace only the local `DISCORD_BOT_TOKEN` value, restart the container, and verify guild command and DM operation. Never print or commit the token.

- [ ] **Step 8: Commit final wiring and docs**

```bash
git add internal/app internal/providers cmd/provider-smoke README.md docs/provider-contracts/cgv.md
git commit -m "feat: enable fixed cgv and megabox alerts"
```

- [ ] **Step 9: Final repository verification**

Run:

```bash
git status --short
git log --oneline -10
```

Expected: no uncommitted implementation changes and all feature commits visible.
