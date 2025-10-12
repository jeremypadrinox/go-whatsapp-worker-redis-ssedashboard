FROM golang:1.21-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

# Build with optimizations for production
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags='-w -s' -o gowhatsapp-worker main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata curl

RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

WORKDIR /app

COPY --from=builder /app/gowhatsapp-worker .

RUN mkdir -p /app/logs /app/tmp && chown -R appuser:appgroup /app

USER appuser

EXPOSE 8081

# Health check for circuit breaker monitoring
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8081/health || exit 1

CMD ["./gowhatsapp-worker", "start"]
