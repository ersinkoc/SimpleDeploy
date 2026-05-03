.PHONY: build test clean release docker docker-build docker-push lint

BINARY := simpledeploy
VERSION := 0.0.8
LDFLAGS := -s -w
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
