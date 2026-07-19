FROM golang:1.26.5-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/my-movie ./cmd/my-movie \
    && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/register-commands ./cmd/register-commands \
    && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/provider-smoke ./cmd/provider-smoke \
    && mkdir -p /out/data \
    && chown -R 65532:65532 /out/data

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder --chown=65532:65532 /out/data /data
COPY --from=builder /out/my-movie /my-movie
COPY --from=builder /out/register-commands /register-commands
COPY --from=builder /out/provider-smoke /provider-smoke

USER 65532:65532
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD ["/my-movie", "healthcheck"]
ENTRYPOINT ["/my-movie"]
