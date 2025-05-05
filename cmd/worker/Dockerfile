# --- Build Stage ---
FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY internal/ ./internal/

COPY cmd/worker/ ./cmd/worker/

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /worker-server ./cmd/worker

# --- Final Stage ---
FROM alpine:latest

WORKDIR /

COPY --from=builder /worker-server /worker-server

CMD ["/worker-server"]
