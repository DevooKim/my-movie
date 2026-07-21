# Public Alert Channels and Status Channel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 영화관 알림 채널을 서버 구성원에게 공개 읽기 전용으로 제공하고, 상세 오류는 비공개 제어 채널에 유지하면서 공개 서버 상태 채널에는 요약 상태를 전송한다.

**Architecture:** `installations`에 상태 채널 ID를 저장하고 초기화 서비스가 공개 카테고리, 비공개 제어 채널, 공개 상태 및 영화관 채널을 생성한다. Discord 권한 생성은 공개와 비공개 함수로 분리하며, 오류 상태 서비스가 한 번의 상태 전이마다 상세 제어 메시지와 요약 공개 메시지를 전송한 후 상태를 저장한다.

**Tech Stack:** Go 1.26, discordgo, SQLite, modernc SQLite driver, Go testing

---

### Task 1: 상태 채널 ID 영속화

**Files:**
- Create: `internal/database/migrations/006_status_channel.sql`
- Modify: `internal/database/repository.go`
- Test: `internal/database/repository_test.go`

- [ ] **Step 1: 실패하는 저장·조회 테스트 작성**

`TestInstallationAndTargetStatePersistAcrossReopen`의 설치 값에 `StatusChannelID: "status"`를 추가하고, 재개방 뒤 구조체 전체 비교가 상태 채널까지 검증하도록 한다.

- [ ] **Step 2: 테스트 실패 확인**

Run: `go test ./internal/database -run TestInstallationAndTargetStatePersistAcrossReopen`

Expected: `Installation`에 `StatusChannelID`가 없어 컴파일 실패한다.

- [ ] **Step 3: 최소 마이그레이션과 저장 구현**

마이그레이션은 기존 행을 보존하며 빈 기본값을 추가한다.

```sql
ALTER TABLE installations ADD COLUMN status_channel_id TEXT NOT NULL DEFAULT '';
```

`Installation`에 다음 필드를 추가하고 `SaveInstallation`, `GetInstallation`의 INSERT, UPDATE, SELECT, Scan에 같은 순서로 연결한다.

```go
StatusChannelID string
```

- [ ] **Step 4: 데이터베이스 테스트 통과 확인**

Run: `go test ./internal/database`

Expected: PASS

### Task 2: 공개 읽기 전용 Discord 권한

**Files:**
- Modify: `internal/discordbot/control.go`
- Test: `internal/discordbot/control_test.go`

- [ ] **Step 1: 공개 권한 실패 테스트 작성**

`publicOverwrites("guild", "bot")` 결과가 `@everyone`에 `ViewChannel`, `ReadMessageHistory`, `AddReactions`를 허용하고 `SendMessages`, `CreatePublicThreads`, `CreatePrivateThreads`, `SendMessagesInThreads`를 거부하는지 검증한다. 봇에는 보기, 읽기, 쓰기, 채널 관리 권한이 허용되어야 한다.

- [ ] **Step 2: 테스트 실패 확인**

Run: `go test ./internal/discordbot -run 'Test(Public|Private)Overwrites'`

Expected: `publicOverwrites`가 없어 컴파일 실패한다.

- [ ] **Step 3: 공개 채널 생성 함수 구현**

`ChannelManager`에 아래 두 메서드를 추가한다.

```go
EnsurePublicCategory(ctx context.Context, guildID, existingID, name string) (string, error)
EnsurePublicTextChannel(ctx context.Context, guildID, categoryID, existingID, name string) (string, error)
```

두 함수는 기존 ID가 같은 길드의 올바른 채널 타입이면 이름, 부모, 권한을 갱신하고, 아니면 새 채널을 생성한다. `privateOverwrites`는 제어 채널에 그대로 사용한다.

- [ ] **Step 4: Discord 권한 테스트 통과 확인**

Run: `go test ./internal/discordbot`

Expected: PASS

### Task 3: 초기화 채널 구조 변경

**Files:**
- Modify: `internal/control/service.go`
- Modify: `internal/control/service_test.go`
- Modify: `internal/discordbot/bot.go`
- Modify: `internal/discordbot/commands.go`

- [ ] **Step 1: 새 구조 실패 테스트 작성**

초기화 테스트가 공개 카테고리, 비공개 `제어`, 공개 `서버-상태`, 다섯 공개 영화관 채널 호출을 구분해서 기록하도록 fake를 확장한다. 생성 순서는 `제어`, `서버-상태`, 다섯 영화관 채널이며 반환 설치 정보의 `StatusChannelID`가 비어 있지 않아야 한다. 두 번째 초기화는 기존 상태 채널 ID를 재사용해야 한다.

- [ ] **Step 2: 테스트 실패 확인**

Run: `go test ./internal/control`

Expected: 현재 서비스가 비공개 카테고리와 영화관 채널만 생성하므로 실패한다.

- [ ] **Step 3: 초기화 서비스 구현**

`Channels` 인터페이스를 공개 카테고리 및 공개 텍스트 채널 메서드와 비공개 텍스트 채널 메서드로 구성한다. `Initialize`는 다음 순서로 보장한다.

```text
EnsurePublicCategory
EnsurePrivateTextChannel("제어")
EnsurePublicTextChannel("서버-상태")
EnsurePublicTextChannel(각 영화관 채널)
SaveInstallation
UpsertPanel
SaveInstallation
```

명령 설명과 성공 응답은 공개 알림 채널 및 비공개 제어 패널을 준비했다는 내용으로 변경한다.

- [ ] **Step 4: 제어 서비스 테스트 통과 확인**

Run: `go test ./internal/control ./internal/discordbot`

Expected: PASS

### Task 4: 상세 제어 알림과 공개 상태 알림 동시 전송

**Files:**
- Create: `internal/database/migrations/007_poll_alert_deliveries.sql`
- Modify: `internal/database/repository.go`
- Test: `internal/database/repository_test.go`
- Modify: `internal/pollalert/service.go`
- Test: `internal/pollalert/service_test.go`

- [ ] **Step 1: 상태 메시지 실패 테스트 작성**

fake messenger가 채널 ID와 내용을 함께 기록하도록 바꾼다. 첫 CGV 실패에서 다음 두 메시지를 검증한다.

```text
control: ⚠️ CGV · 용산아이파크몰 조회 실패\nupstream failed
status: ⚠️ CGV · 용산아이파크몰 조회가 원활하지 않습니다
```

연속 실패는 추가 메시지가 없어야 하며, 복구에서는 두 채널에 각각 기존 상세 복구 메시지와 공개 복구 메시지가 한 번씩 있어야 한다. `StatusChannelID`가 빈 기존 설치에서는 제어 메시지만 전송되는 테스트도 추가한다.

- [ ] **Step 2: 테스트 실패 확인**

Run: `go test ./internal/pollalert`

Expected: 상태 채널 메시지가 없어 실패한다.

- [ ] **Step 3: 상태 전이 전송 구현**

상세 제어 메시지는 현재 형식을 유지한다. 공개 실패 메시지는 원문 오류를 사용하지 않고 다음 형식만 사용한다.

```go
fmt.Sprintf("⚠️ %s · %s 조회가 원활하지 않습니다", provider, theaterName)
```

복구 공개 메시지는 `조회가 정상화되었습니다` 형식으로 전송한다. `poll_alert_states`에 `control_delivered`, `status_delivered`를 추가하고 기존 행은 모두 전달 완료로 마이그레이션한다. 두 채널 전송을 모두 시도하고 오류는 `errors.Join`으로 반환한다. 채널별 전달 결과를 저장하여 다음 동일 상태 조회에서는 실패한 채널만 재시도한다. 장애 메시지가 전달되지 않은 채널에는 해당 장애의 복구 메시지를 보내지 않는다. 상태 채널 ID가 없으면 공개 상태 전달을 완료한 것으로 취급하고 기존 제어 채널 동작만 수행한다.

- [ ] **Step 4: 오류 알림 테스트 통과 확인**

Run: `go test ./internal/pollalert`

Expected: PASS

### Task 5: 문서와 전체 회귀 검증

**Files:**
- Modify: `README.md`

- [ ] **Step 1: 운영 문서 갱신**

채널 트리에 `서버-상태`를 추가하고, 제어 채널은 비공개이며 나머지 채널은 구성원이 읽고 반응할 수 있는 읽기 전용 채널이라고 설명한다. 상세 오류는 제어 채널, 요약 오류 및 복구 상태는 서버 상태 채널에 전송된다고 기록한다.

- [ ] **Step 2: 포맷과 전체 테스트 실행**

Run: `gofmt -w internal/database/repository.go internal/database/repository_test.go internal/discordbot/control.go internal/discordbot/control_test.go internal/control/service.go internal/control/service_test.go internal/discordbot/bot.go internal/discordbot/commands.go internal/pollalert/service.go internal/pollalert/service_test.go`

Run: `go test ./...`

Expected: 모든 패키지 PASS

- [ ] **Step 3: 변경 범위 검토**

Run: `git diff --check && git status --short`

Expected: 공백 오류가 없고 계획된 파일만 변경된다.

- [ ] **Step 4: 구현 커밋**

```bash
git add README.md internal/database/migrations/006_status_channel.sql internal/database/migrations/007_poll_alert_deliveries.sql internal/database/repository.go internal/database/repository_test.go internal/discordbot/control.go internal/discordbot/control_test.go internal/control/service.go internal/control/service_test.go internal/discordbot/bot.go internal/discordbot/commands.go internal/pollalert/service.go internal/pollalert/service_test.go docs/superpowers/plans/2026-07-21-public-alert-channels-and-status.md
git commit -m "feat: 공개 알림 채널과 서버 상태 추가"
```
