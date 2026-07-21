# Guide Image First Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Discord `안내` 채널에서 이미지 전용 메시지를 먼저 표시하고 안내 본문과 참여 버튼 메시지를 그 아래 표시한다.

**Architecture:** 설치 정보에 이미지 메시지 ID를 별도로 저장한다. 초기화 서비스가 이미지 메시지를 먼저 보장하고 저장한 뒤 본문 메시지 upsert에 이미지 ID를 전달한다. Discord 어댑터는 저장되지 않은 이미지·본문 메시지도 채널에서 복구하고, 본문이 이미지보다 오래됐을 때만 재생성해 순서와 중복 방지를 유지한다.

**Tech Stack:** Go 1.26, discordgo, SQLite, modernc SQLite driver, Go testing

---

### Task 1: 이미지 메시지 ID 영속화

**Files:**
- Create: `internal/database/migrations/009_guide_image_message.sql`
- Modify: `internal/database/repository.go`
- Test: `internal/database/repository_test.go`

- [ ] **Step 1: 재개방 영속화 테스트에 이미지 메시지 ID 추가**

`TestInstallationAndTargetStatePersistAcrossReopen`의 설치 값에 다음 필드를 추가한다.

```go
GuideImageMessageID: "guide-image-message",
```

- [ ] **Step 2: 데이터베이스 테스트가 새 필드 부재로 실패하는지 확인**

Run: `go test ./internal/database -run TestInstallationAndTargetStatePersistAcrossReopen`
Expected: `Installation`에 `GuideImageMessageID`가 없어 컴파일 실패

- [ ] **Step 3: 마이그레이션과 repository 구현**

```sql
ALTER TABLE installations ADD COLUMN guide_image_message_id TEXT NOT NULL DEFAULT '';
```

`Installation`, `SaveInstallation`, `GetInstallation`에 `GuideImageMessageID string`을 추가하고 최신 마이그레이션 회귀 기대값을 `9`로 변경한다.

- [ ] **Step 4: 데이터베이스 테스트 통과 확인**

Run: `go test ./internal/database`
Expected: PASS

### Task 2: 이미지와 안내 본문 메시지 분리

**Files:**
- Modify: `internal/discordbot/control.go`
- Test: `internal/discordbot/control_test.go`

- [ ] **Step 1: 메시지 구성 실패 테스트 작성**

다음을 독립적으로 검증한다.

```text
guideImageMessage: Content와 Components가 비어 있고 PNG Files가 한 개
guideMessage: 안내 본문과 alerts:join 버튼이 있고 Files는 비어 있음
```

- [ ] **Step 2: 기존 단일 메시지 구현에서 실패 확인**

Run: `go test ./internal/discordbot -run 'TestGuideImageMessage|TestGuideMessage'`
Expected: `guideImageMessage`가 없고 `guideMessage.Files`가 비어 있지 않아 실패

- [ ] **Step 3: 이미지 메시지 upsert 구현**

`Channels` 어댑터에 다음 메서드를 추가한다.

```go
UpsertGuideImage(ctx context.Context, channelID, existingID string) (messageID string, created bool, err error)
```

`guideImageMessage()`는 내장 PNG만 반환한다. `guideMessage()`에서는 `Files`를 제거한다. 저장된 이미지 ID가 없으면 봇이 작성한 이미지 전용 메시지를 찾아 복구하고, 기존 ID가 있으면 첨부를 교체하며, 복구할 메시지가 없으면 생성한다. `AllowedMentions`는 두 메시지 모두 비운다.

- [ ] **Step 4: Discord 메시지 테스트 통과 확인**

Run: `go test ./internal/discordbot`
Expected: PASS

### Task 3: 초기화 순서와 기존 메시지 전환

**Files:**
- Modify: `internal/control/service.go`
- Modify: `internal/control/service_test.go`

- [ ] **Step 1: 초기화 순서 실패 테스트 작성**

fake channel manager의 호출 순서가 다음과 같은지 검증한다.

```text
guide-image → 기존 guide 삭제 → guide
```

신규 설치에서는 `guide-image → guide`이고, 재초기화에서는 두 기존 ID가 재사용되어 삭제나 신규 생성 없이 갱신되어야 한다. Discord 메시지 후보 선택은 이미지보다 최신인 봇의 `alerts:join` 메시지를 우선해, 본문 생성 후 ID 저장 전에 중단된 경우에도 기존 본문을 복구한다.

- [ ] **Step 2: 서비스 테스트 실패 확인**

Run: `go test ./internal/control -run TestInitialize`
Expected: 이미지 메시지 단계와 기존 안내 메시지 전환이 없어 실패

- [ ] **Step 3: 이미지 우선 초기화 구현**

`Channels`에 `UpsertGuideImage`를 추가하고 `UpsertGuide`에 이미지 메시지 ID 인자를 추가해 다음 순서로 초기화한다.

```text
1. GuideImageMessageID로 이미지 메시지 upsert
2. 설치 정보 저장
3. 안내 본문과 버튼 메시지를 이미지 ID와 함께 upsert
4. 저장된 본문 또는 채널에서 복구한 본문이 이미지보다 오래됐으면 어댑터가 삭제
5. 이미지보다 최신인 기존 본문이 있으면 재사용하고 없으면 새 본문 생성
6. 설치 정보 저장
```

각 새 메시지 직후 DB 저장이 실패하면 기존 `created` 플래그와 분리된 cleanup context를 사용해 방금 만든 메시지만 삭제한다.

- [ ] **Step 4: 제어 서비스 테스트 통과 확인**

Run: `go test ./internal/control`
Expected: PASS

### Task 4: 문서, 전체 검증과 배포

**Files:**
- Modify: `README.md`

- [ ] **Step 1: README 안내 구조 갱신**

`안내` 채널 설명을 이미지 전용 메시지와 본문·버튼 메시지 두 개가 순서대로 게시되는 구조로 변경한다.

- [ ] **Step 2: 포맷과 전체 테스트 실행**

Run: `gofmt -w internal/database/repository.go internal/database/repository_test.go internal/discordbot/control.go internal/discordbot/control_test.go internal/control/service.go internal/control/service_test.go`

Run: `go test ./... && go vet ./...`
Expected: PASS

- [ ] **Step 3: 변경 범위 확인**

Run: `git diff --check && git status --short`
Expected: 계획된 파일만 변경되고 공백 오류가 없음

- [ ] **Step 4: 구현 커밋과 push**

```bash
git add README.md docs/superpowers/plans/2026-07-22-guide-image-first.md internal/database/migrations/009_guide_image_message.sql internal/database/repository.go internal/database/repository_test.go internal/discordbot/control.go internal/discordbot/control_test.go internal/control/service.go internal/control/service_test.go
git commit -m "feat: 안내 이미지를 본문 위에 표시"
git push origin main
```

- [ ] **Step 5: 로컬 서비스 재배포**

```bash
docker compose up --build -d
docker compose exec -T app /register-commands
curl -fsS http://127.0.0.1:3001/health
```

Expected: 새 이미지로 컨테이너가 재기동되고 health가 `ok`
