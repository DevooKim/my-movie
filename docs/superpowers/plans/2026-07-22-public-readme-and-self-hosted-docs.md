# Public README and Self-hosted Documentation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 저장소 README를 일반 사용자용 서비스 소개로 바꾸고 개발 및 운영 문서를 `docs/self-hosted.md`로 분리한다.

**Architecture:** `README.md`는 서비스의 목적, 지원 특별관, 참여 방법과 알림 내용만 설명한다. 기존 설치, 설정, 배포, 폴링, 상태 확인 및 개발 정보는 `docs/self-hosted.md`가 단독으로 제공하며 README에서 링크한다.

**Tech Stack:** GitHub Flavored Markdown, Docker Compose, Podman Compose, Go 1.26.5

---

### Task 1: 일반 사용자용 README 작성

**Files:**
- Modify: `README.md`

- [x] **Step 1: 상단 이미지와 서비스 제목 배치**

`internal/discordbot/assets/my-movie-pepe.png`를 상대 경로 이미지로 표시하고 제목을 `🏢 영화예매감시국`으로 변경한다.

- [x] **Step 2: 일반 사용자용 내용만 작성**

서비스 소개, `🔔 알림 채널 보기` 버튼을 통한 참여 방법, 알림 정보와 서버 상태 메시지를 설명한다. 지원 영화관의 이름이나 개수는 나열하지 않는다.

- [x] **Step 3: self-hosted 문서 연결**

직접 서버를 운영하려는 독자에게 `docs/self-hosted.md` 링크만 제공하고 환경변수나 실행 명령은 README에 두지 않는다.

### Task 2: 개발 및 운영 문서 분리

**Files:**
- Create: `docs/self-hosted.md`

- [x] **Step 1: 기존 기술 정보 이동**

Discord 준비, Bot 권한, 환경변수, 채널 구조, 초기화 명령, 기준선, 폴링, 오류 처리, 앱 실행 링크, health 및 Provider 점검, Go 검증 명령을 기존 의미대로 옮긴다.

- [x] **Step 2: Docker와 Podman 실행 방법 명시**

두 환경 모두 먼저 `my-movie-data` 외부 볼륨을 생성하고 Compose 서비스를 실행하도록 작성한다. Lightpanda 이미지는 `docker.io/lightpanda/browser:nightly`로 완전히 수식된 현재 `compose.yml`을 사용한다.

- [x] **Step 3: SQLite 영속화 설명**

외부 볼륨이 컨테이너의 `/data`에 연결되고 기본 DB 파일이 `/data/my-movie.sqlite`임을 명시한다.

### Task 3: 정적 문서 검증과 커밋

**Files:**
- Verify: `README.md`
- Verify: `docs/self-hosted.md`

- [x] **Step 1: 문서 경계 확인**

README에 환경변수 표, 컨테이너 명령, 개발 검증 명령이 남지 않았고 self-hosted 문서에 기존 기술 정보가 보존됐는지 diff로 확인한다.

- [x] **Step 2: 링크와 금지 내용 확인**

README의 이미지 및 self-hosted 상대 경로가 실제 파일을 가리키고 외부 레퍼런스 저장소 이름이 문서에 포함되지 않았는지 확인한다.

- [x] **Step 3: 자동 테스트 생략**

사용자 요청에 따라 Go 테스트와 컨테이너 실행은 수행하지 않는다.

- [x] **Step 4: 커밋**

```bash
git add README.md docs/self-hosted.md docs/superpowers/plans/2026-07-22-public-readme-and-self-hosted-docs.md
git commit -m "docs: 사용자 안내와 셀프 호스팅 문서 분리"
```
