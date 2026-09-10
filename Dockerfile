FROM golang:1.25-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -v -ldflags="-s -w" -o /bin/anythingllm-gateway .
RUN CGO_ENABLED=0 GOOS=linux go build -v -ldflags="-s -w" -o /bin/anythingllm-sync ./cmd/anythingllm-sync

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /bin/anythingllm-gateway /usr/local/bin/
COPY --from=builder /bin/anythingllm-sync /usr/local/bin/

VOLUME ["/data"]

RUN adduser -D -u 1000 appuser && chown -R appuser:appuser /app

USER appuser

ENTRYPOINT ["anythingllm-gateway"]
