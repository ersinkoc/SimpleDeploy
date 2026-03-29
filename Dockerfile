# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /simpledeploy .

# Runtime stage
FROM alpine:3.19

RUN apk add --no-cache git curl docker-cli docker-cli-compose

COPY --from=builder /simpledeploy /usr/local/bin/simpledeploy

ENTRYPOINT ["simpledeploy"]
CMD ["help"]
