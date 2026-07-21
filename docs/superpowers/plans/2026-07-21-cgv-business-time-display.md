# CGV Business Time Display Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Display CGV after-midnight showtimes using the same business date and 24-47 hour notation as the CGV app.

**Architecture:** Keep CGV's raw screening date as the alert play date and only insert a colon into the validated four-digit time. External IDs and duplicate detection remain unchanged.

**Tech Stack:** Go, standard library testing.

---

### Task 1: Preserve CGV business date and extended hour

**Files:**
- Modify: `internal/providers/cgv/provider_test.go`
- Modify: `internal/providers/cgv/provider.go`
- Modify: `internal/database/repository_test.go`
- Modify: `internal/database/repository.go`

- [ ] **Step 1: Write failing business-time tests**

```go
func TestNormalizeDateTimePreservesCGVBusinessDateAndExtendedHour(t *testing.T) {
    for _, test := range []struct{ raw, want string }{{"2400", "24:00"}, {"2510", "25:10"}} {
        date, clock, err := normalizeDateTime("20260809", test.raw)
        if err != nil || date != "2026-08-09" || clock != test.want {
            t.Fatalf("raw=%s date=%s clock=%s err=%v", test.raw, date, clock, err)
        }
    }
}
```

- [ ] **Step 2: Run the test and verify it fails**

Run: `go test ./internal/providers/cgv -run TestNormalizeDateTimePreservesCGVBusinessDateAndExtendedHour -count=1`

Expected: FAIL because `2400` currently becomes the next day at `00:00`.

- [ ] **Step 3: Preserve the validated raw hour**

```go
return date.Format("2006-01-02"), fmt.Sprintf("%02d:%02d", hour, minute), nil
```

Update `target_showtimes` conflicts so `play_date`, `starts_at`, and `ends_at` are refreshed without changing the baseline or creating another delivery.

- [ ] **Step 4: Run provider and full tests**

Run: `go test ./internal/providers/cgv -count=1 && go test ./...`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/providers/cgv/provider.go internal/providers/cgv/provider_test.go internal/database/repository.go internal/database/repository_test.go docs/superpowers/specs/2026-07-21-cgv-business-time-display-design.md docs/superpowers/plans/2026-07-21-cgv-business-time-display.md
git commit -m "fix: CGV 영업일 시각 표기 유지"
```
