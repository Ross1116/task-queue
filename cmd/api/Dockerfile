FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY internal/ ./internal/
COPY cmd/api/ ./cmd/api/

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /api-server ./cmd/api

# --- Final Stage ---
# Use a minimal Alpine image for the final stage
FROM alpine:latest

WORKDIR /

COPY --from=builder /api-server /api-server

EXPOSE 8080

CMD ["/api-server"]
