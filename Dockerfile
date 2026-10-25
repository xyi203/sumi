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

EXPOSE 3000

# Set AUTO_MIGRATE=true to apply migrations before the server starts listening.
ENTRYPOINT ["/app/server"]
