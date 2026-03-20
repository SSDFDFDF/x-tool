FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/x-tool ./cmd/x-tool

FROM alpine:3.21

WORKDIR /app

COPY --from=builder /out/x-tool /app/x-tool
RUN mkdir -p /app/data /app/logs

EXPOSE 8026

ENV X_TOOL_PORT=8026
ENV X_TOOL_HOST=0.0.0.0
ENV X_TOOL_DB_PATH=/app/data.db
ENV X_TOOL_LOG_PATH=/app/logs/app.log

ENTRYPOINT ["/app/x-tool"]
