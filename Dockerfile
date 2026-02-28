FROM golang:1.23-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o gem2api ./cmd/server

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /app/gem2api .

# Data directory for SQLite database
RUN mkdir -p /app/data
VOLUME ["/app/data"]

EXPOSE 8080
ENV DB_PATH=/app/data/gem2api.db
CMD ["./gem2api"]
