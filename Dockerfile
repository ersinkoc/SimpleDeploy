# Build stage
# Pinned to the toolchain in go.mod and in .github/workflows/ci.yml. Keep the
# three in step: a tag that does not exist on the registry fails the image
# build with a confusing "manifest unknown".
FROM golang:1.23-alpine AS builder

WORKDIR /app
# SimpleDeploy has no external module dependencies, so go.sum may not exist.
# The bracket glob matches it when present without failing when it is absent.
COPY go.mod go.su[m] ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /simpledeploy .

# Runtime stage
FROM alpine:3.21

# git: clone/pull app repositories.
# docker-cli + compose plugin: the service shells out to both.
# ca-certificates: without them every HTTPS clone fails on certificate verify.
RUN apk add --no-cache git curl ca-certificates docker-cli docker-cli-compose

COPY --from=builder /simpledeploy /usr/local/bin/simpledeploy

ENTRYPOINT ["simpledeploy"]
CMD ["help"]
