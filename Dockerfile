FROM golang:1.27-alpine3.23 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/server ./main.go

FROM alpine:3.23

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /out/server /app/server
COPY --from=builder /src/migrations /app/migrations
COPY --from=builder /src/scripts/docker-entrypoint.sh /app/docker-entrypoint.sh

RUN chmod +x /app/docker-entrypoint.sh

EXPOSE 3000

ENTRYPOINT ["/app/docker-entrypoint.sh"]
