.PHONY: build test test-coverage race clean release docker docker-build docker-push lint verify fmt

BINARY := simpledeploy
VERSION := 0.1.0
MODULE  := github.com/ersinkoc/SimpleDeploy
# -X stamps the version into the binary so `simpledeploy version` always
# matches this file. Without it the two drifted apart silently.
LDFLAGS := -s -w -X $(MODULE)/internal/cli.version=$(VERSION)
REGISTRY := ghcr.io/ersinkoc

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

test:
	go test -p=1 -count=1 ./...

test-coverage:
	go test -p=1 -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | grep "^total:"

lint:
	go vet ./...

fmt:
	gofmt -w .

# Race detector — matches .github/workflows/race.yml
race:
	go test -race -p=1 -count=1 ./...

# Everything CI runs, plus a binary smoke test. Reports all failures, not just
# the first. Run this before pushing.
verify:
	sh scripts/verify.sh

clean:
	rm -f $(BINARY) coverage.out
	rm -rf dist/

# Cross-compile all release binaries
release: clean
	@mkdir -p dist
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-arm64 .
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-arm64 .
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-windows-amd64.exe .
	@echo "Release binaries in dist/"

# Build Docker image (local only)
docker-build:
	docker build -t $(BINARY):latest .

# Build and push to GHCR
docker:
	docker build -t $(REGISTRY)/$(BINARY):$(VERSION) .
	docker build -t $(REGISTRY)/$(BINARY):latest .
	docker push $(REGISTRY)/$(BINARY):$(VERSION)
	docker push $(REGISTRY)/$(BINARY):latest
