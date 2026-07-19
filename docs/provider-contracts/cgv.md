# CGV Provider Contract

Status: blocked

Verified on: 2026-07-19

## Gate result

A signed-out visit to the official CGV site was rejected before a usable movie, theater, or showtime response could be observed. A plain Go `net/http` request from `golang:1.26.5-alpine`, with a 10-second context timeout and no persisted session state, reproduced the rejection:

```text
GET https://www.cgv.co.kr/
HTTP 403
Content-Type: text/html; charset=UTF-8
```

Because the public entry request is denied, an unauthenticated catalog and showtime contract cannot be verified for the production container. The CGV Provider is therefore excluded from the enabled Provider registry.

## Reproduction

Run a small Go program in `golang:1.26.5-alpine` that constructs the request with `http.NewRequestWithContext`, uses the default TLS verification, and prints only the response status and content type. The result above was obtained without a browser profile or saved session data.

## Enablement condition

CGV support may be enabled only after official movie, theater, and showtime responses can be reproduced through unauthenticated plain Go HTTP requests both locally and in the production container. The service must not add access-control workarounds, a bundled browser, or third-party relay infrastructure.
