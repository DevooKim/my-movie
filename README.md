# my-movie

한 Discord 서버에서 다섯 개 특별관의 예매 오픈을 감시하고, 참여 역할이 있는 구성원에게 각 특별관 전용 채널로 알림을 게시하는 Go 서비스입니다. 영화 등록 없이 지점 전체 시간표를 확인하며 상태와 전달 이력은 SQLite에 보존합니다.

## 지원 대상

| 전용 채널 | 감시 범위 |
| --- | --- |
| `메가박스-코엑스-돌비` | 코엑스 Dolby Cinema |
| `메가박스-남현아-돌비` | 남양주현대아울렛 스페이스원 Dolby Cinema |
| `cgv-용산-imax` | 용산아이파크몰 IMAX |
| `cgv-용산-4dx` | 용산아이파크몰 4DX |
| `cgv-용산-screenx` | 용산아이파크몰 SCREENX |

메가박스는 비인증 HTTPS JSON 요청을 사용합니다. CGV는 같은 Compose 프로젝트의 Lightpanda에서 공식 사이트 요청을 실행합니다. Provider용 쿠키나 client ID는 필요하지 않습니다.

## Discord 준비

Developer Portal의 Installation에서 다음을 설정합니다.

- Installation Contexts: `Guild Install`
- Guild Install Scopes: `applications.commands`, `bot`
- Bot Permissions: `Manage Channels`, `Manage Roles`, `View Channels`, `Send Messages`, `Read Message History`

생성된 설치 링크로 사용할 서버에 Bot을 추가합니다. Application Command를 실행할 사용자에게는 `Use Application Commands` 권한도 있어야 합니다.

Bot token은 `.env`나 배포 환경의 secret으로만 관리하고 저장소에 커밋하지 마세요.

## 환경변수

| 이름 | 필수 | 기본값 | 설명 |
| --- | --- | --- | --- |
| `DISCORD_BOT_TOKEN` | 예 | - | Discord Bot token |
| `DISCORD_APPLICATION_ID` | 예 | - | Discord Application ID |
| `DISCORD_GUILD_ID` | 예 | - | 명령을 등록할 단일 서버 ID |
| `APP_LAUNCH_BASE_URL` | 아니요 | - | 영화관 앱 실행 Lambda Function URL (`https://...`) |
| `DATABASE_PATH` | 아니요 | `/data/my-movie.sqlite` | SQLite 파일 경로 |
| `POLL_INTERVAL_SECONDS` | 아니요 | `180` | 폴링 주기(초) |
| `PORT` | 아니요 | `3000` | 컨테이너 내부 health 포트 |
| `TZ` | 아니요 | `Asia/Seoul` | 상영일 계산 시간대 |

## 실행

처음 한 번 volume을 만들고 서비스를 빌드합니다.

```bash
docker volume create my-movie-data
docker compose up --build -d
```

길드 명령 세트를 `/알림` 하나로 교체합니다.

```bash
docker compose exec -T app /register-commands
```

Discord에서 `/알림 초기화`를 실행하면 다음 구조가 생성됩니다.

```text
공지
안내
영화 예매 알림
├─ 제어
├─ 서버-상태
├─ 메가박스-코엑스-돌비
├─ 메가박스-남현아-돌비
├─ cgv-용산-imax
├─ cgv-용산-4dx
└─ cgv-용산-screenx
```

`공지`와 `안내`는 모든 구성원이 볼 수 있는 최상위 읽기 전용 채널입니다. `안내`에는 이미지 전용 메시지가 먼저 게시되고, 그 아래 안내 본문과 `🔔 알림 채널 보기` 버튼이 표시됩니다. 버튼을 누르면 Bot이 `영화 알림` 역할을 부여하고 알림 카테고리를 표시합니다. Bot 역할은 `영화 알림` 역할보다 위에 있어야 합니다.

알림 카테고리의 `서버-상태`와 영화관 채널은 역할을 받은 구성원이 읽고 이모지 반응을 추가할 수 있지만 메시지나 스레드를 작성할 수 없습니다. `제어`는 초기화한 사용자와 Bot만 볼 수 있으며 소유자도 일반 메시지는 작성할 수 없습니다. 제어 메시지의 선택 메뉴에서 대상을 고른 뒤 `알림 켜기` 또는 `알림 끄기` 버튼을 누릅니다. Discord 관리자는 플랫폼 권한에 따라 제한을 우회할 수 있습니다.

운영 공지는 소유자가 `/알림 공지 내용:<메시지>`로 게시합니다. 입력에 멘션 문법이 있어도 실제 멘션은 전송되지 않습니다. 초기화, 공지, 대상 선택과 ON/OFF는 설치 소유자만 사용할 수 있습니다.

대상을 켤 때 현재 회차를 기준선으로 저장하므로 이미 열린 회차는 알리지 않습니다. 껐다 다시 켜도 새 기준선을 만들며, 꺼진 동안 열린 회차는 건너뜁니다.

폴링은 앱 시작 시점이 아니라 3분 경계에 맞춰 실행됩니다. 정상 버스트는 경계 시각부터 `+0`, `+15`, `+30`, `+45초`에 총 네 번 조회합니다. 한 시도가 늦어져도 남은 시도는 건너뛰지 않고 즉시 실행합니다. 메가박스 조회가 실패하면 다음 조회는 실패가 끝난 시점에서 정확히 6분 뒤로 미뤄지며, 그 재시도 버스트도 기준 시각부터 15초 간격으로 네 번 조회합니다. CGV CDP·Lightpanda 오류와 내부 오류는 다음 3분 버스트에서 바로 재시도합니다. CGV 탭은 최초 조회 전 5초 동안 준비한 뒤 같은 CDP 세션을 계속 재사용합니다. 세션 오류가 발생한 경우에만 다음 주기에 새 탭을 만들며, 서비스 종료 시 탭과 연결을 닫습니다. 메가박스는 같은 시각에 HTTP로 조회합니다.

## 알림

새 회차는 해당 특별관 채널에 일반 Markdown 메시지로 게시됩니다. 영화 제목, 날짜, 시간만 굵게 표시하며 Embed는 사용하지 않습니다. Provider가 좌석 정보를 제공하면 알림 생성 시점의 잔여석과 총 좌석을 표시합니다. 앱 실행 버튼도 함께 제공합니다.

`APP_LAUNCH_BASE_URL`을 설정하면 CGV 앱 버튼은 `<base>/cgv`, 메가박스 앱 버튼은 `<base>/megabox`를 사용합니다. Lambda는 각각 `cgv://`, `megaboxapp://`로 리다이렉트해야 합니다. 설정하지 않으면 기존 모바일 예매 URL을 사용합니다.

동일한 회차 ID는 재시작 후에도 다시 전송하지 않습니다. 일시적인 Discord 오류는 세 번까지 재시도합니다. 대상 채널이 삭제되거나 Bot이 접근할 수 없으면 해당 대상은 자동으로 OFF가 됩니다.

공급자 조회가 실패하면 비공개 `제어` 채널에 상세 오류를 한 번 보내고, 역할 사용자에게 보이는 `서버-상태` 채널에는 상세 원인을 제외한 상태 메시지를 보냅니다. 연속 실패는 중복 알리지 않으며 이후 조회가 성공하면 두 채널에 정상화 알림을 한 번 보냅니다.

## 상태 확인과 Provider 점검

호스트의 health 포트는 `3001`입니다.

```bash
curl -fsS http://127.0.0.1:3001/health
docker compose ps
```

활성 대상이 없으면 health는 200을 반환합니다. 활성 Provider가 폴링 주기의 두 배 동안 성공하지 못했거나 DB 연결이 실패하면 503을 반환합니다.

각 Provider는 영화 목록을 순회하지 않고 구성된 첫 지점 하나만 제한적으로 점검합니다.

```bash
docker compose exec -T app /provider-smoke megabox
docker compose exec -T app /provider-smoke cgv
```

## 개발 검증

Go 1.26.5가 필요합니다.

```bash
go test ./...
go test -race ./...
go vet ./...
```

Docker만 있는 환경에서는 다음과 같이 실행할 수 있습니다.

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.26.5-bookworm go test ./...
docker run --rm -v "$PWD":/src -w /src golang:1.26.5-bookworm go test -race ./...
docker run --rm -v "$PWD":/src -w /src golang:1.26.5-bookworm go vet ./...
```
