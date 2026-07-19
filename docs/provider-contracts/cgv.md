# CGV Provider Contract

Status: enabled through Lightpanda

Verified on: 2026-07-19

## Runtime boundary

CGV의 공식 사이트는 일반 서버 HTTP 요청을 거부하므로 앱 컨테이너가 Lightpanda의 CDP WebSocket에 연결합니다. Lightpanda는 공식 CGV origin을 연 뒤 그 브라우저 컨텍스트에서 날짜 및 상영 시간표 API를 호출합니다.

Compose의 `lightpanda` 서비스에는 128MB 메모리 제한을 둡니다. 앱은 쿠키, 사용자 계정, 저장된 브라우저 프로필, 외부 중계 서버를 사용하지 않습니다.

## Requests

용산아이파크몰 지점 `0013`에 대해 먼저 상영 가능 날짜를 받고, 각 날짜의 전체 시간표를 한 번씩 조회합니다. 응답의 특별관 코드로 다음 대상을 분리합니다.

| 코드 | 대상 |
| --- | --- |
| `02` | 4DX |
| `03` | IMAX |
| `04` | SCREENX |

한 폴링 주기에서 같은 지점과 날짜를 대상별로 중복 요청하지 않습니다.

## Normalized fields

회차 ID는 지점, 상영일, 상영관 번호, 회차 순번, 영화 번호의 조합으로 만듭니다. 영화명, 영문명, 지점, 상영관, 특별관 형식, 상영일, 시작·종료 시각, 관람 등급, 잔여·총 좌석과 포스터 경로를 정규화합니다.

좌석 값이 없거나 숫자가 아니면 회차 자체는 유지하고 좌석 정보만 알 수 없음으로 처리합니다. 시작·종료 시각의 24시 이상 표기는 다음 날 시각으로 정규화합니다.

## Bounded verification

실시간 계약 점검은 영화 검색이나 영화별 반복을 수행하지 않습니다.

```bash
docker compose exec -T app /provider-smoke cgv
```

결과는 Provider, 지점, 특별관 전체 회차 수와 한 개의 정규화 샘플만 출력합니다.
