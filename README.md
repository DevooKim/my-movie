# my-movie

한 Discord 서버에서 영화와 극장 지점을 등록해 두고, 새 예매 회차가 열리면 사용자 DM으로 알려주는 Go 서비스입니다. 현재 Megabox를 지원하며 SQLite에 구독과 발송 상태를 저장합니다.

## 현재 Provider 상태

| Provider | 상태 | 설명 |
| --- | --- | --- |
| Megabox | 사용 가능 | 공개 예매 API를 비인증 HTTPS 요청으로 조회합니다. |
| CGV | 비활성 | 공식 사이트가 무상태 Go 요청을 HTTP 403으로 거부해 안전한 공개 요청 계약을 확인하지 못했습니다. |

CGV 상태의 재검증 조건은 [계약 문서](docs/provider-contracts/cgv.md)에 기록되어 있습니다. 접근제어 우회, 장기 세션 값, 브라우저 자동화, 중계 서버는 사용하지 않습니다.

## Discord 준비

1. [Discord Developer Portal](https://discord.com/developers/applications)에서 애플리케이션과 Bot을 만듭니다.
2. Bot token, Application ID, 알림 명령을 설치할 서버의 Guild ID를 준비합니다.
3. OAuth2 URL Generator에서 `bot`, `applications.commands` scope를 선택해 Bot을 서버에 초대합니다. Bot에는 `View Channels`, `Send Messages` 권한만 부여하면 됩니다.
4. 알림을 받을 사용자는 해당 서버의 개인정보 보호 설정에서 서버 멤버의 DM을 허용해야 합니다. Discord 오류 50007이 발생하면 그 사용자의 구독은 자동 비활성화됩니다.

Bot token은 `.env`나 배포 환경의 secret으로만 관리하고 저장소에 커밋하지 마세요.

## 환경변수

`.env.example`을 `.env`로 복사하고 값을 채웁니다.

| 이름 | 필수 | 기본값 | 설명 |
| --- | --- | --- | --- |
| `DISCORD_BOT_TOKEN` | 예 | - | Discord Bot token |
| `DISCORD_APPLICATION_ID` | 예 | - | Discord Application ID |
| `DISCORD_GUILD_ID` | 예 | - | 명령을 등록할 단일 서버 ID |
| `DATABASE_PATH` | 아니요 | `/data/my-movie.sqlite` | SQLite 파일 경로 |
| `POLL_INTERVAL_SECONDS` | 아니요 | `300` | 폴링 주기(초, 양수) |
| `PORT` | 아니요 | `3000` | health HTTP 포트 |
| `TZ` | 아니요 | `Asia/Seoul` | 상영일 계산 시간대 |

Provider용 쿠키나 client ID는 필요하지 않습니다.

## Docker 실행

이미지를 빌드합니다.

```bash
docker build -t my-movie-alert:latest .
```

먼저 길드 명령을 등록합니다. 이 작업은 구성된 길드의 명령 세트를 `/알림` 하나로 교체합니다.

```bash
docker run --rm --env-file .env --entrypoint /register-commands my-movie-alert:latest
```

SQLite 파일을 보존할 named volume과 함께 서비스를 실행합니다.

```bash
docker volume create my-movie-data
docker run -d \
  --name my-movie \
  --restart unless-stopped \
  --env-file .env \
  -p 3000:3000 \
  -v my-movie-data:/data \
  my-movie-alert:latest
```

`POLL_INTERVAL_SECONDS`를 바꾼 뒤 컨테이너를 재생성하면 폴링 주기를 조절할 수 있습니다. 한 Provider의 동시 요청은 최대 2개이며 폴링 사이클은 겹쳐 실행되지 않습니다.

## Discord 명령

- `/알림 등록`: 영화관, 지점, 영화를 자동완성으로 선택합니다. 기존 회차를 기준선으로 저장한 뒤 확인 DM을 보냅니다.
- `/알림 목록`: 본인이 등록한 알림만 표시합니다.
- `/알림 삭제`: 본인의 알림 하나를 삭제합니다.
- `/알림 전체삭제`: 본인의 알림을 모두 삭제합니다.
- `/알림 도움말`: 간단한 사용법을 표시합니다.

새 회차 알림은 영화, 지점, 상영일, 시간, 상영관을 날짜별로 묶어 전송합니다. 메시지에는 앱 실행 후 미설치 시 스토어로 연결되는 공식 HTTPS 예약 경로와 웹 예약 링크가 함께 들어갑니다. 같은 회차는 재시작 후에도 다시 보내지 않습니다.

## 상태 확인과 점검

```bash
curl -i http://localhost:3000/health
docker inspect --format '{{json .State.Health}}' my-movie
```

활성 구독이 없으면 `/health`는 200을 반환합니다. 활성 Provider가 최근 폴링 주기의 두 배 동안 한 번도 성공하지 못했거나 DB 연결이 실패하면 503을 반환합니다. 응답에는 사용자나 구독 정보가 포함되지 않습니다.

DB migration만 검사하려면 Discord 설정 없이 실행할 수 있습니다.

```bash
docker run --rm -v my-movie-data:/data my-movie-alert:latest database-check
```

실제 Provider 공개 계약을 점검합니다.

```bash
docker run --rm --entrypoint /provider-smoke my-movie-alert:latest megabox
docker run --rm --entrypoint /provider-smoke my-movie-alert:latest cgv
```

Megabox는 카탈로그와 정규화된 회차 요약을 출력합니다. CGV는 비활성 상태인 동안 종료 코드 2를 반환합니다.

## 백업

일관된 SQLite 백업을 위해 먼저 컨테이너를 중지한 뒤 volume 파일을 복사합니다.

```bash
docker stop my-movie
docker run --rm -v my-movie-data:/data -v "$PWD":/backup alpine \
  cp /data/my-movie.sqlite /backup/my-movie.sqlite.backup
docker start my-movie
```

복구 시에는 서비스를 중지하고 백업 파일을 `/data/my-movie.sqlite`로 되돌린 뒤 다시 시작합니다. `-wal`, `-shm` 파일을 따로 복사하는 대신 위처럼 서비스를 중지한 상태의 기본 DB 파일을 백업하세요.

## 개발 검증

Go 1.26.5가 필요합니다.

```bash
go test ./...
go test -race ./...
go vet ./...
```

Docker만 있는 환경에서는 공식 Go 이미지로 같은 검사를 실행할 수 있습니다.

```bash
docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26.5-alpine go test ./...
docker run --rm -v "$PWD":/workspace -w /workspace golang:1.26.5-bookworm go test -race ./...
```
