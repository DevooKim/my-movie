# Role-Gated Alert Onboarding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 신규 구성원에게 공개 `공지`·`안내`만 보여주고, 안내 버튼으로 `영화 알림` 역할을 받은 사용자에게 읽기 전용 알림 카테고리를 열며, 소유자 공지 명령을 제공한다.

**Architecture:** 설치 정보에 역할·최상위 채널·안내 메시지 ID를 저장한다. 제어 서비스가 Discord 역할과 채널 리소스를 단계별로 보장하고 즉시 저장하며, Discord 봇 핸들러가 참여 버튼과 소유자 공지 하위 명령을 서비스 메서드로 전달한다.

**Tech Stack:** Go 1.26, discordgo, SQLite, modernc SQLite driver, Go testing

---

### Task 1: 온보딩 리소스 ID 영속화

**Files:**
- Create: `internal/database/migrations/008_role_gated_onboarding.sql`
- Modify: `internal/database/repository.go`
- Test: `internal/database/repository_test.go`

- [ ] **Step 1: 설치 정보 저장 실패 테스트 작성**

`TestInstallationAndTargetStatePersistAcrossReopen`의 `Installation` 값에 다음 필드를 추가하고 구조체 전체 비교로 재개방 후 값을 검증한다.

```go
ViewerRoleID    string
NoticeChannelID string
GuideChannelID  string
GuideMessageID  string
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `go test ./internal/database -run TestInstallationAndTargetStatePersistAcrossReopen`

Expected: 새 필드가 없어 컴파일 실패한다.

- [ ] **Step 3: 마이그레이션과 repository 구현**

```sql
ALTER TABLE installations ADD COLUMN viewer_role_id TEXT NOT NULL DEFAULT '';
ALTER TABLE installations ADD COLUMN notice_channel_id TEXT NOT NULL DEFAULT '';
ALTER TABLE installations ADD COLUMN guide_channel_id TEXT NOT NULL DEFAULT '';
ALTER TABLE installations ADD COLUMN guide_message_id TEXT NOT NULL DEFAULT '';
```

`Installation`, `SaveInstallation`, `GetInstallation`에 네 필드를 동일한 순서로 추가한다. 마이그레이션 회귀 테스트의 최신 버전을 `8`로 갱신한다.

- [ ] **Step 4: 데이터베이스 테스트 통과 확인**

Run: `go test ./internal/database`

Expected: PASS

### Task 2: 역할 제한 권한과 Discord 리소스 관리

**Files:**
- Modify: `internal/discordbot/control.go`
- Test: `internal/discordbot/control_test.go`
- Create: `internal/discordbot/assets/my-movie-pepe.png`

- [ ] **Step 1: 역할 제한 권한 실패 테스트 작성**

`restrictedOverwrites(guildID, viewerRoleID, botID)`가 다음 계약을 만족하는지 검증한다.

```text
@everyone: ViewChannel, SendMessages, thread permissions 거부
영화 알림 role: ViewChannel, ReadMessageHistory, AddReactions 허용; SendMessages, thread permissions 거부
bot member: ViewChannel, ReadMessageHistory, SendMessages, ManageChannels와 overwrite 관리에 필요한 thread permissions 허용
```

`privateOverwrites`에서 소유자의 `SendMessages`가 허용되지 않고 보기·기록 읽기만 허용되는 테스트도 추가한다.

- [ ] **Step 2: 권한 테스트 실패 확인**

Run: `go test ./internal/discordbot -run 'TestRestrictedOverwrites|TestPrivateOverwrites'`

Expected: `restrictedOverwrites`가 없고 소유자에게 쓰기 권한이 남아 있어 실패한다.

- [ ] **Step 3: 역할·채널·메시지 API 구현**

`channelSession`에 `GuildChannels`, `GuildRoles`, `GuildRoleCreate`, `GuildMemberRoleAdd`를 추가하고 `ChannelManager`에 다음 메서드를 구현한다.

```go
EnsureViewerRole(ctx context.Context, guildID, existingID, name string) (string, error)
EnsurePublicTextChannel(ctx context.Context, guildID, categoryID, existingID, name string) (string, error)
EnsureRestrictedCategory(ctx context.Context, guildID, existingID, name, viewerRoleID string) (string, error)
EnsureRestrictedTextChannel(ctx context.Context, guildID, categoryID, existingID, name, viewerRoleID string) (string, error)
UpsertGuide(ctx context.Context, channelID, existingID string) (string, error)
AddMemberRole(ctx context.Context, guildID, userID, roleID string) error
SendAnnouncement(ctx context.Context, channelID, content string) error
```

저장된 ID가 유효하지 않으면 길드의 역할 또는 채널 목록에서 이름과 타입이 같은 리소스를 찾아 재사용한 뒤 새 리소스 생성을 최후 수단으로 사용한다. 안내와 공지 메시지는 `AllowedMentions`에서 모든 parse를 비워 실제 멘션을 차단한다. 안내 메시지는 실행 파일에 내장한 PNG와 함께 생성하거나 갱신한다.

- [ ] **Step 4: Discord 리소스 테스트 통과 확인**

Run: `go test ./internal/discordbot`

Expected: PASS

### Task 3: 초기화 순서와 단계별 저장

**Files:**
- Modify: `internal/control/service.go`
- Modify: `internal/control/service_test.go`

- [ ] **Step 1: 초기화 구조 실패 테스트 작성**

fake channel manager가 역할, 최상위 채널, 제한 카테고리와 하위 채널 호출을 기록하게 한다. 초기화 결과가 다음 리소스를 포함하는지 검증한다.

```text
viewer role: 영화 알림
top-level public: 공지, 안내
restricted category: 영화 예매 알림
private child: 제어
restricted children: 서버-상태, 다섯 영화관 채널
guide message: 안내 채널에 한 개
```

fake store의 저장 이력을 기록해 역할, 공지, 안내, 카테고리 생성 후 각각 ID가 즉시 저장되는지 검증한다. 두 번째 초기화에서는 새 리소스가 생성되지 않고 저장된 ID를 재사용해야 한다.

- [ ] **Step 2: 초기화 테스트 실패 확인**

Run: `go test ./internal/control -run TestInitialize`

Expected: 현재 공개 카테고리 구조와 단일 최종 저장 때문에 실패한다.

- [ ] **Step 3: 안전한 초기화 구현**

초기화 순서는 다음으로 고정한다.

```text
1. 기존 제어 채널 비공개 보장
2. 영화 알림 역할 보장 후 SaveInstallation
3. 공지 채널 보장 후 SaveInstallation
4. 안내 채널 보장 후 SaveInstallation
5. 안내 메시지 보장 후 SaveInstallation
6. 역할 제한 카테고리 보장 후 SaveInstallation
7. 제어, 서버 상태, 다섯 대상 채널 권한 갱신
8. 제어 패널 갱신 후 SaveInstallation
```

신규 설치에는 기존 제어 채널이 없으므로 2~5단계가 먼저 실행된다. 기존 설치는 제어 채널을 먼저 잠가 공개 노출을 막는다.

- [ ] **Step 4: 제어 서비스 테스트 통과 확인**

Run: `go test ./internal/control`

Expected: PASS

### Task 4: 참여 버튼과 소유자 공지 서비스

**Files:**
- Modify: `internal/control/service.go`
- Modify: `internal/control/service_test.go`

- [ ] **Step 1: 참여·공지 실패 테스트 작성**

다음 동작을 독립 테스트로 추가한다.

```text
JoinAlerts: 설치의 guild_id, viewer_role_id로 요청 사용자에게 역할 부여
JoinAlerts: 초기화 전 또는 역할 ID가 없으면 오류
Announce: 소유자만 notice_channel_id에 "📢 **공지**\n<내용>" 전송
Announce: 비소유자, 빈 내용, 초기화 전 호출 거부
```

- [ ] **Step 2: 서비스 테스트 실패 확인**

Run: `go test ./internal/control -run 'TestJoinAlerts|TestAnnounce'`

Expected: 메서드가 없어 컴파일 실패한다.

- [ ] **Step 3: 최소 서비스 구현**

`Channels` 인터페이스에 역할 부여와 공지 전송 메서드를 연결하고 다음 공개 메서드를 추가한다.

```go
JoinAlerts(ctx context.Context, userID string) error
Announce(ctx context.Context, ownerID, content string) error
```

`Announce`는 `strings.TrimSpace`, 빈 문자열 거부, 소유자 검증 후 공지 전송만 수행한다. 멘션 차단은 Discord 어댑터가 담당한다.

- [ ] **Step 4: 참여·공지 서비스 테스트 통과 확인**

Run: `go test ./internal/control`

Expected: PASS

### Task 5: Discord 명령과 버튼 처리

**Files:**
- Modify: `internal/discordbot/commands.go`
- Modify: `internal/discordbot/bot.go`
- Modify: `internal/discordbot/bot_test.go`
- Modify: `internal/discordbot/control_test.go`

- [ ] **Step 1: 명령·버튼 실패 테스트 작성**

`Command()`가 `초기화`와 문자열 필수 옵션 `내용`을 가진 `공지` 하위 명령을 제공하는지 검증한다. 안내 메시지가 custom ID `alerts:join`과 라벨 `🔔 알림 채널 보기` 버튼을 갖는지 검증한다.

Bot fake controller로 다음 라우팅을 검증한다.

```text
/알림 공지 내용 → Controller.Announce(userID, content)
alerts:join → Controller.JoinAlerts(userID)
```

- [ ] **Step 2: 라우팅 테스트 실패 확인**

Run: `go test ./internal/discordbot`

Expected: 공지 옵션과 참여 버튼 라우팅이 없어 실패한다.

- [ ] **Step 3: 명령 및 interaction 구현**

`Controller`에 `JoinAlerts`, `Announce`를 추가한다. 참여 버튼은 공개 사용자가 누를 수 있으며 성공과 실패를 ephemeral 메시지로 응답한다. 공지 명령도 defer 후 성공 또는 오류를 ephemeral 응답한다. 기존 대상 선택 및 ON/OFF 동작은 유지한다.

- [ ] **Step 4: Discord 봇 테스트 통과 확인**

Run: `go test ./internal/discordbot`

Expected: PASS

### Task 6: 운영 문서와 전체 검증

**Files:**
- Modify: `README.md`

- [ ] **Step 1: README 갱신**

최상위 `공지`·`안내`, 역할 제한 카테고리, 참여 버튼, `/알림 공지`, `Manage Roles` 권한 요구사항과 일반 사용자의 읽기·반응 전용 정책을 기록한다. 안내 문구는 운영체제를 특정하지 않는다.

- [ ] **Step 2: 포맷과 전체 테스트 실행**

Run: `gofmt -w internal/database/repository.go internal/database/repository_test.go internal/discordbot/control.go internal/discordbot/control_test.go internal/control/service.go internal/control/service_test.go internal/discordbot/commands.go internal/discordbot/bot.go internal/discordbot/bot_test.go`

Run: `go test ./...`

Expected: 모든 패키지 PASS

- [ ] **Step 3: 변경 범위 검토**

Run: `git diff --check && git status --short`

Expected: 공백 오류가 없고 계획된 파일만 변경된다.

- [ ] **Step 4: 구현 커밋**

```bash
git add README.md docs/superpowers/plans/2026-07-22-role-gated-alert-onboarding.md internal/database/migrations/008_role_gated_onboarding.sql internal/database/repository.go internal/database/repository_test.go internal/discordbot/control.go internal/discordbot/control_test.go internal/control/service.go internal/control/service_test.go internal/discordbot/commands.go internal/discordbot/bot.go internal/discordbot/bot_test.go
git commit -m "feat: 역할 기반 알림 참여와 공지 추가"
```
