# Channel Target Alerts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace movie subscriptions and DMs with five private target channels, owner-only controls, theater-wide polling, and plain Markdown booking-open alerts.

**Architecture:** Persist one Discord installation plus five target states instead of per-user movie subscriptions. Poll each physical branch once per date, split its response into fixed premium targets, compare stable showtime IDs against target baselines, and publish new sessions to the target channel.

**Tech Stack:** Go 1.26, DiscordGo message components, SQLite migrations, Lightpanda CDP for CGV, Megabox JSON API, Docker Compose

---

## File structure

- Create `internal/database/migrations/003_channel_targets.sql`: replace subscription delivery state with installation, target, showtime, baseline, and channel-delivery tables.
- Create `internal/control/service.go` and `service_test.go`: installation, owner authorization, channel reconciliation, and ON/OFF transactions.
- Create `internal/discordbot/control.go` and `control_test.go`: category/channel creation and owner-only component panel.
- Modify `internal/domain/{types.go,provider.go}`: branch snapshot contract and enriched showtime fields.
- Modify `internal/providers/{megabox,cgv}`: fetch all branch sessions once and normalize alert metadata.
- Rewrite `internal/scheduler/scheduler.go`: group enabled targets by physical branch and share snapshots.
- Rewrite `internal/notification/service.go` and `internal/discordbot/notifier.go`: channel delivery and plain Markdown formatting.
- Modify `internal/app/app.go`, `cmd/register-commands/main.go`, `README.md`, and `compose.yml`: wire the new model and document permissions and operation.
- Remove obsolete movie-registration paths after replacement tests pass: `internal/subscription`, movie autocomplete handlers, DM confirmation code, and old delivery queries.

### Task 1: Persist installation and target state

**Files:**
- Create: `internal/database/migrations/003_channel_targets.sql`
- Modify: `internal/database/repository.go`
- Modify: `internal/database/repository_test.go`

- [ ] **Step 1: Write failing migration and repository tests**

Add tests that reopen a version-2 database and assert exactly five target rows can be stored independently, installation ownership survives restart, and baselines can be replaced atomically:

```go
installation := database.Installation{GuildID: "g1", OwnerUserID: "u1", CategoryID: "cat", ControlChannelID: "control", ControlMessageID: "message"}
if err := repository.SaveInstallation(ctx, installation); err != nil { t.Fatal(err) }
if err := repository.SaveTargetState(ctx, database.TargetState{TargetID: "cgv-yongsan-imax", ChannelID: "imax", Enabled: true}); err != nil { t.Fatal(err) }
if err := repository.ReplaceBaseline(ctx, "cgv-yongsan-imax", []string{"s1", "s2"}); err != nil { t.Fatal(err) }
```

- [ ] **Step 2: Run the database tests and verify RED**

Run: `go test ./internal/database`

Expected: FAIL because `Installation`, `TargetState`, and baseline repository methods do not exist.

- [ ] **Step 3: Implement migration 3 and repository methods**

Create these tables and stop using legacy subscriptions for polling:

```sql
CREATE TABLE installations (
  guild_id TEXT PRIMARY KEY,
  owner_user_id TEXT NOT NULL,
  category_id TEXT NOT NULL,
  control_channel_id TEXT NOT NULL,
  control_message_id TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);

CREATE TABLE target_states (
  target_id TEXT PRIMARY KEY,
  channel_id TEXT NOT NULL,
  enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
  updated_at TEXT NOT NULL
);

CREATE TABLE target_baselines (
  target_id TEXT NOT NULL REFERENCES target_states(target_id) ON DELETE CASCADE,
  showtime_id TEXT NOT NULL,
  first_seen_at TEXT NOT NULL,
  PRIMARY KEY(target_id, showtime_id)
);

CREATE TABLE target_showtimes (
  target_id TEXT NOT NULL REFERENCES target_states(target_id) ON DELETE CASCADE,
  showtime_id TEXT NOT NULL,
  provider TEXT NOT NULL,
  theater_id TEXT NOT NULL,
  theater_name TEXT NOT NULL,
  movie_id TEXT NOT NULL,
  movie_name TEXT NOT NULL,
  movie_english_name TEXT NOT NULL DEFAULT '',
  play_date TEXT NOT NULL,
  starts_at TEXT NOT NULL,
  ends_at TEXT NOT NULL,
  auditorium TEXT NOT NULL,
  format TEXT NOT NULL,
  rating TEXT NOT NULL DEFAULT '',
  remaining_seats INTEGER NOT NULL DEFAULT 0,
  total_seats INTEGER NOT NULL DEFAULT 0,
  seat_count_known INTEGER NOT NULL CHECK (seat_count_known IN (0, 1)),
  poster_url TEXT NOT NULL DEFAULT '',
  first_seen_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  PRIMARY KEY(target_id, showtime_id)
);

CREATE TABLE channel_deliveries (
  target_id TEXT NOT NULL,
  showtime_id TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pending', 'sent', 'failed')),
  attempt_count INTEGER NOT NULL DEFAULT 0,
  last_error TEXT,
  PRIMARY KEY(target_id, showtime_id),
  FOREIGN KEY(target_id, showtime_id)
    REFERENCES target_showtimes(target_id, showtime_id) ON DELETE CASCADE
);
```

Implement `SaveInstallation`, `GetInstallation`, `SaveTargetState`, `ListTargetStates`, `ReplaceBaseline`, `RecordTargetSnapshot`, and `ListPendingChannelDeliveries` with SQLite transactions. `RecordTargetSnapshot` upserts normalized showtime metadata before creating its delivery row, so notification retries do not depend on another provider request. Migration 3 leaves legacy tables intact for rollback safety but no new code reads them.

- [ ] **Step 4: Run database tests and verify GREEN**

Run: `go test ./internal/database`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/database
git commit -m "feat: persist channel target configuration"
```

### Task 2: Add branch-wide provider snapshots

**Files:**
- Modify: `internal/domain/types.go`
- Modify: `internal/domain/provider.go`
- Modify: `internal/providers/megabox/{response.go,transport.go,provider.go,provider_test.go}`
- Modify: `internal/providers/cgv/{response.go,transport.go,provider.go,provider_test.go}`

- [ ] **Step 1: Write failing provider tests**

Assert one Megabox branch request returns every `DBC` movie and one CGV site response splits into the three target codes. Assert normalized metadata includes end time and seats:

```go
snapshot, err := provider.FetchBranchSnapshot(ctx, domain.Branch{Provider: domain.ProviderCGV, TheaterID: "0013"})
if err != nil { t.Fatal(err) }
imax := snapshot.ForTarget("cgv-yongsan-imax")
if len(imax) != 1 || imax[0].MovieName != "호프" || imax[0].RemainingSeats != 57 || imax[0].TotalSeats != 144 {
    t.Fatalf("imax=%+v", imax)
}
```

- [ ] **Step 2: Run provider tests and verify RED**

Run: `go test ./internal/providers/megabox ./internal/providers/cgv`

Expected: FAIL because the provider interface is movie-specific and `Showtime` lacks alert metadata.

- [ ] **Step 3: Implement the snapshot contract**

Use these domain types consistently:

```go
type Branch struct {
    Provider ProviderID
    TheaterID string
    AreaCode string
}

type Showtime struct {
    Provider ProviderID
    TargetID, TheaterID, TheaterName string
    MovieID, MovieName, MovieEnglishName string
    ExternalID, PlayDate, StartsAt, EndsAt string
    Auditorium, Format, Rating string
    RemainingSeats, TotalSeats int
    SeatCountKnown bool
    PosterURL string
}

type TheaterProvider interface {
    ID() ProviderID
    FetchBranchSnapshot(context.Context, Branch) ([]Showtime, error)
    BuildBookingLinks(AlertTarget, string) BookingLinks
}
```

For Megabox, send branch selection without `arrMovieNo` and keep `theabKindCd=DBC`. Decode `movieNm`, `movieEngNm`, `playEndTime`, `restSeatCnt`, `totSeatCnt`, `admisClassCdNm`, `playKindNm`, `moviePosterImg`, `boxoRank`, `boxoBokdRt`, and event fields.

For CGV, fetch each advertised date once for site `0013`, map `tcscnsGradCd` values `02`, `03`, `04`, and decode `movNm`, `engProdNm`, `scnendTm`, `frSeatCnt`, `stcnt`, `cratgClsNm`, `movkndDsplNm`, and poster path. Treat invalid seat strings as unknown rather than rejecting the whole snapshot.

- [ ] **Step 4: Run provider tests and verify GREEN**

Run: `go test ./internal/providers/megabox ./internal/providers/cgv`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain internal/providers
git commit -m "feat: fetch theater wide premium schedules"
```

### Task 3: Build owner-only Discord initialization

**Files:**
- Create: `internal/control/service.go`
- Create: `internal/control/service_test.go`
- Create: `internal/discordbot/control.go`
- Create: `internal/discordbot/control_test.go`
- Modify: `internal/discordbot/commands.go`

- [ ] **Step 1: Write failing initialization tests**

Use a fake Discord session to assert `/알림 초기화` creates one category, one control channel, and the five exact target channels with permission overwrites:

```go
denyEveryone := discordgo.PermissionOverwrite{ID: "g1", Type: discordgo.PermissionOverwriteTypeRole, Deny: discordgo.PermissionViewChannel}
allowOwner := discordgo.PermissionOverwrite{ID: "u1", Type: discordgo.PermissionOverwriteTypeMember, Allow: discordgo.PermissionViewChannel | discordgo.PermissionSendMessages}
```

Assert a second initialization reuses existing channels and recreates only a missing saved channel.

- [ ] **Step 2: Run control tests and verify RED**

Run: `go test ./internal/control ./internal/discordbot`

Expected: FAIL because channel initialization and the new command do not exist.

- [ ] **Step 3: Implement initialization and command definition**

Replace registration subcommands with one owner command:

```go
&discordgo.ApplicationCommand{
    Name: "알림",
    Options: []*discordgo.ApplicationCommandOption{
        {Type: discordgo.ApplicationCommandOptionSubCommand, Name: "초기화", Description: "비공개 알림 채널과 제어 패널을 만듭니다"},
    },
}
```

Create category `영화 예매 알림`, control channel `제어`, and the five names from the target catalog. Persist every returned ID. Reject initialization when an installation already belongs to a different owner.

- [ ] **Step 4: Run control tests and verify GREEN**

Run: `go test ./internal/control ./internal/discordbot`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/control internal/discordbot
git commit -m "feat: initialize private theater channels"
```

### Task 4: Add the persistent control panel and ON/OFF baseline

**Files:**
- Modify: `internal/control/service.go`
- Modify: `internal/control/service_test.go`
- Modify: `internal/discordbot/control.go`
- Modify: `internal/discordbot/control_test.go`
- Modify: `internal/discordbot/bot.go`

- [ ] **Step 1: Write failing component tests**

Assert the panel contains one target select and two buttons with stable IDs:

```go
const (
    targetSelectID = "alerts:target"
    enableButtonID = "alerts:enable"
    disableButtonID = "alerts:disable"
)
```

Assert a non-owner interaction is rejected, enabling fetches one current branch snapshot and stores it as baseline without sending, and disabling excludes the target immediately.

- [ ] **Step 2: Run component tests and verify RED**

Run: `go test ./internal/control ./internal/discordbot`

Expected: FAIL because component handling and target state transitions do not exist.

- [ ] **Step 3: Implement panel and transitions**

Render a plain control message with five status lines and Discord components. A target-select interaction edits that same message so its ON/OFF button custom IDs contain the selected target, for example `alerts:enable:cgv-yongsan-imax`. The following button interaction therefore identifies the target without process-local selection state. Re-render the five status lines and buttons after every transition.

Implement `Enable(ctx, ownerID, targetID)` as one serialized operation: validate owner, fetch the target branch snapshot, filter to the target, replace its baseline, then set `enabled=1`. Implement `Disable` by setting `enabled=0`; the next enable replaces the baseline.

- [ ] **Step 4: Run component tests and verify GREEN**

Run: `go test ./internal/control ./internal/discordbot`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/control internal/discordbot
git commit -m "feat: control theater alerts with buttons"
```

### Task 5: Poll enabled branches and create channel deliveries

**Files:**
- Rewrite: `internal/scheduler/scheduler.go`
- Rewrite: `internal/scheduler/scheduler_test.go`
- Modify: `internal/database/repository.go`
- Modify: `internal/database/repository_test.go`

- [ ] **Step 1: Write failing scheduler tests**

Enable all three CGV targets and assert `FetchBranchSnapshot` is called once for 용산. Enable both Megabox targets and assert two branch calls. Assert existing baseline IDs produce no delivery and new IDs produce one pending delivery for the matching target only.

- [ ] **Step 2: Run scheduler tests and verify RED**

Run: `go test ./internal/scheduler ./internal/database`

Expected: FAIL because polling is still grouped by movie subscriptions.

- [ ] **Step 3: Implement branch grouping and snapshot recording**

Group enabled states by `(provider, theaterID)` from the fixed target catalog. Fetch each group once, split rows by `TargetID`, and transactionally upsert every normalized row into `target_showtimes` before inserting pending channel deliveries only for IDs absent from `target_baselines`. Always add current IDs to the baseline after successful comparison. Leave baselines untouched on provider errors.

- [ ] **Step 4: Run scheduler tests and verify GREEN**

Run: `go test ./internal/scheduler ./internal/database`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/scheduler internal/database
git commit -m "feat: poll enabled theater channels"
```

### Task 6: Send plain Markdown alerts to target channels

**Files:**
- Rewrite: `internal/notification/service.go`
- Rewrite: `internal/notification/service_test.go`
- Rewrite: `internal/discordbot/notifier.go`
- Rewrite: `internal/discordbot/notifier_test.go`

- [ ] **Step 1: Write failing formatter and delivery tests**

Assert the notifier calls `ChannelMessageSendComplex(target.ChannelID, message)` without `Embeds`, groups sessions by movie and date, and formats only title, date, and time in bold:

```go
want := "🎬 새 예매 회차 오픈\n\n**호프**\n📅 **2026년 7월 19일**\n⏰ **19:10 – 21:56**\n💺 잔여 57 / 144석\n\nCGV 용산아이파크몰 · IMAX"
if message.Content != want { t.Fatalf("content=%q", message.Content) }
if len(message.Embeds) != 0 { t.Fatal("unexpected embed") }
```

- [ ] **Step 2: Run notification tests and verify RED**

Run: `go test ./internal/notification ./internal/discordbot`

Expected: FAIL because notifications still create DMs and embeds.

- [ ] **Step 3: Implement channel delivery**

Build content from normalized showtimes. For multiple sessions, print one bold time line per session with its seat snapshot. Omit the seat line when `SeatCountKnown` is false. Keep HTTPS booking links as Discord link buttons, which are message components rather than embeds. Mark deliveries sent only after the channel API succeeds; retry transient failures up to the existing three-attempt limit. If Discord reports a missing or forbidden target channel, set that target OFF.

- [ ] **Step 4: Run notification tests and verify GREEN**

Run: `go test ./internal/notification ./internal/discordbot`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/notification internal/discordbot
git commit -m "feat: post booking alerts to theater channels"
```

### Task 7: Remove movie subscriptions and wire the new application

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`
- Modify: `cmd/register-commands/main.go`
- Modify: `cmd/provider-smoke/main.go`
- Delete: `internal/subscription/service.go`
- Delete: `internal/subscription/service_test.go`
- Modify: `internal/health/server.go`
- Modify: `internal/health/server_test.go`

- [ ] **Step 1: Write failing application wiring tests**

Assert the app constructs `control.Service`, the branch scheduler, and channel notifier, and no component depends on `subscription.Service`. Assert health considers only enabled target providers and recent branch poll success.

- [ ] **Step 2: Run application tests and verify RED**

Run: `go test ./internal/app ./internal/health ./cmd/...`

Expected: FAIL because the app still wires movie subscriptions and the old smoke command.

- [ ] **Step 3: Wire the channel model and remove obsolete paths**

Connect the repository, target catalog, both providers, control service, component bot, branch scheduler, channel notification service, and health handler. Remove movie autocomplete, registration, list, delete, delete-all, registration confirmation, and DM creation code. Update provider smoke to fetch one branch snapshot per provider without iterating movies.

- [ ] **Step 4: Run application tests and verify GREEN**

Run: `go test ./internal/app ./internal/health ./cmd/...`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app internal/health internal/subscription internal/discordbot cmd
git commit -m "feat: replace movie subscriptions with channel controls"
```

### Task 8: Update operations and verify live behavior

**Files:**
- Modify: `README.md`
- Modify: `compose.yml`
- Modify: `docs/provider-contracts/cgv.md`

- [ ] **Step 1: Update runtime documentation and permissions**

Document `Manage Channels`, `View Channels`, `Send Messages`, `Read Message History`, and `Use Application Commands`. Replace movie registration documentation with `/알림 초기화`, the private channel tree, owner-only controls, ON/OFF baseline semantics, port `3001`, and the Lightpanda sidecar.

- [ ] **Step 2: Run complete static verification**

Run:

```bash
gofmt -w cmd internal
go test ./...
go test -race ./...
go vet ./...
git diff --check
```

Expected: every command exits 0 with no warnings.

- [ ] **Step 3: Run bounded live provider checks**

Run one branch request per provider:

```bash
docker compose exec -T app /provider-smoke megabox
docker compose exec -T app /provider-smoke cgv
```

Expected: each command prints provider, branch, total premium sessions, and one sanitized sample without scanning movies or returning HTTP 429.

- [ ] **Step 4: Rebuild, register the command, and verify health**

Run:

```bash
docker compose up --build -d
docker compose exec -T app /register-commands
curl -fsS http://127.0.0.1:3001/health
```

Expected: command registration prints `알림`, health returns `{"status":"ok"}`, and exactly one app plus one Lightpanda container is running.

- [ ] **Step 5: Verify the Discord owner flow**

Run `/알림 초기화` as the owner, confirm the private category and six channels are created, enable one target, verify no baseline alert is posted, then use a fixture-injected delivery test to prove the next new showtime posts once to that target channel. Confirm another user cannot operate the panel.

- [ ] **Step 6: Commit operations changes**

```bash
git add README.md compose.yml docs/provider-contracts/cgv.md
git commit -m "docs: operate channel based theater alerts"
```

- [ ] **Step 7: Final repository check**

Run:

```bash
git status --short
git log --oneline -12
```

Expected: no uncommitted feature changes remain and the channel-alert commits are visible.
