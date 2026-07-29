package buildpack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type AppType struct {
	Name     string
	Detected bool
	Port     int
}

const (
	TypeNode   = "node"
	TypeGo     = "go"
	TypePHP    = "php"
	TypePython = "python"
	TypeRuby   = "ruby"
	TypeStatic = "static"
	TypeDocker = "dockerfile"
)

func Detect(repoDir string) *AppType {
	// Check for Dockerfile first
	if _, err := os.Stat(filepath.Join(repoDir, "Dockerfile")); err == nil {
		return &AppType{Name: TypeDocker, Detected: true, Port: 3000}
	}

	// Node.js
	if _, err := os.Stat(filepath.Join(repoDir, "package.json")); err == nil {
		port := detectNodePort(repoDir)
		return &AppType{Name: TypeNode, Detected: true, Port: port}
	}

	// Go
	if _, err := os.Stat(filepath.Join(repoDir, "go.mod")); err == nil {
		return &AppType{Name: TypeGo, Detected: true, Port: 8080}
	}

	// Python
	if _, err := os.Stat(filepath.Join(repoDir, "requirements.txt")); err == nil {
		return &AppType{Name: TypePython, Detected: true, Port: 8000}
	}
	if _, err := os.Stat(filepath.Join(repoDir, "pyproject.toml")); err == nil {
		return &AppType{Name: TypePython, Detected: true, Port: 8000}
	}

	// PHP
	if _, err := os.Stat(filepath.Join(repoDir, "composer.json")); err == nil {
		return &AppType{Name: TypePHP, Detected: true, Port: 80}
	}
	// Check for any .php files
	if hasFiles(repoDir, ".php") {
		return &AppType{Name: TypePHP, Detected: true, Port: 80}
	}

	// Ruby
	if _, err := os.Stat(filepath.Join(repoDir, "Gemfile")); err == nil {
		return &AppType{Name: TypeRuby, Detected: true, Port: 3000}
	}

	// Static (HTML)
	if hasFiles(repoDir, ".html") {
		return &AppType{Name: TypeStatic, Detected: true, Port: 80}
	}

	return &AppType{Name: TypeStatic, Detected: false, Port: 80}
}

func detectNodePort(repoDir string) int {
	data, err := os.ReadFile(filepath.Join(repoDir, "package.json"))
	if err != nil {
		return 3000
	}
	content := strings.ToLower(string(data))
	if strings.Contains(content, `"next"`) {
		return 3000
	}
	if strings.Contains(content, `"nuxt"`) {
		return 3000
	}
	return 3000
}

func hasFiles(dir, ext string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ext) {
			return true
		}
	}
	return false
}

func GetDockerfileTemplate(appType string) string {
	switch appType {
	case TypeNode:
		return dockerfileNode
	case TypeGo:
		return dockerfileGo
	case TypePHP:
		return dockerfilePHP
	case TypePython:
		return dockerfilePython
	case TypeRuby:
		return dockerfileRuby
	case TypeStatic:
		return dockerfileStatic
	default:
		return ""
	}
}

func WriteDockerfile(repoDir, appType string) error {
	template := GetDockerfileTemplate(appType)
	if template == "" {
		return fmt.Errorf("no Dockerfile template for type: %s", appType)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "Dockerfile"), []byte(template), 0644); err != nil {
		return err
	}
	// Ship a .dockerignore next to the generated Dockerfile so `.git` and
	// friends stay out of the build context. Never clobber one the project
	// already committed — if the repo has its own, it knows better than we do.
	// A failure here is not fatal to the deploy: the image still builds, it
	// is just larger, so the error is deliberately swallowed.
	ignorePath := filepath.Join(repoDir, ".dockerignore")
	if _, err := os.Stat(ignorePath); os.IsNotExist(err) {
		_ = os.WriteFile(ignorePath, []byte(dockerignoreContent), 0644)
	}
	return nil
}

// The generated Dockerfiles below are written into a repository that
// SimpleDeploy did not author, so every step has to tolerate the shapes real
// projects actually take. Two rules follow from that:
//
//  1. Never COPY a file that may not exist. `COPY package-lock.json ./` fails
//     the build outright for a yarn/pnpm project; the wildcard/`.`-copy forms
//     used here do not.
//  2. Never assume an optional script or lockfile is present. Build steps are
//     guarded by a shell test so a project without a `build` script (or
//     without a lockfile) still produces a working image.
//
// These are deliberately conservative starting points — a project with real
// build requirements should commit its own Dockerfile, which detection
// prefers over every template here.

// dockerfileNode installs the FULL dependency set (including devDependencies)
// because build tooling — tsc, vite, next, webpack — lives there. The previous
// version ran `npm ci --production` and then `npm run build`, which failed on
// essentially every project that had a build step at all. Dev dependencies are
// pruned after the build so the runtime layer stays small.
const dockerfileNode = `FROM node:22-alpine AS builder
WORKDIR /app
COPY package*.json ./
# npm ci requires a lockfile; fall back to install when there isn't one.
RUN if [ -f package-lock.json ]; then npm ci; else npm install; fi
COPY . .
# Run the build script only if the project defines one. 2>&1 matters: some npm
# versions print the script listing on stderr, and without it the grep would
# silently find nothing and skip the build for every project.
RUN if npm run 2>&1 | grep -q '^  build$'; then npm run build; fi
RUN npm prune --omit=dev

FROM node:22-alpine
ENV NODE_ENV=production
WORKDIR /app
COPY --from=builder /app .
EXPOSE 3000
# Prefer the project's own start script; fall back to a conventional entrypoint.
CMD ["sh", "-c", "if npm run 2>&1 | grep -q '^  start$'; then npm start; elif [ -f server.js ]; then node server.js; else node index.js; fi"]
`

// dockerfileGo copies go.mod/go.sum via a wildcard (go.sum is absent in
// dependency-free modules) and installs ca-certificates in the runtime layer —
// without them any outbound HTTPS call from the app fails with a certificate
// error, which is a confusing failure to debug in a scratch-ish image.
// Floating base tag on purpose: a pinned minor (the previous `golang:1.26`)
// breaks every Go deploy the moment that tag does not exist on the registry,
// and it also fails outright for a module whose go directive is newer than the
// pin. `golang:alpine` always resolves and always satisfies the module. A
// project that needs a specific toolchain should commit its own Dockerfile,
// which detection prefers over this template.
const dockerfileGo = `FROM golang:alpine AS builder
WORKDIR /app
COPY go.mod go.su[m] ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /app/server .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/server /server
EXPOSE 8080
CMD ["/server"]
`

// dockerfilePHP runs composer install only when a composer.json is present, so
// plain-PHP projects (detected via a bare *.php file) still build.
//
// Composer is copied from its official image rather than fetched with a
// curl-piped installer: the php:*-apache images do not guarantee curl, and a
// build that depends on a network fetch script fails in exactly the
// environments where it is hardest to diagnose.
const dockerfilePHP = `FROM php:8.3-apache
COPY --from=composer:2 /usr/bin/composer /usr/bin/composer
RUN a2enmod rewrite
WORKDIR /var/www/html
COPY . /var/www/html/
RUN if [ -f composer.json ]; then \
      composer install --no-dev --no-interaction --optimize-autoloader --no-progress; \
    fi
RUN chown -R www-data:www-data /var/www/html
EXPOSE 80
`

// dockerfilePython handles both dependency declarations that Detect accepts.
// The previous version copied requirements.txt unconditionally, so every
// pyproject.toml-only project failed at COPY before installing anything.
const dockerfilePython = `FROM python:3.12-slim
ENV PYTHONUNBUFFERED=1 PIP_NO_CACHE_DIR=1
WORKDIR /app
COPY . .
RUN if [ -f requirements.txt ]; then pip install -r requirements.txt; \
    elif [ -f pyproject.toml ]; then pip install .; fi
EXPOSE 8000
# Prefer a conventional entrypoint; app.py and main.py cover most layouts.
CMD ["sh", "-c", "if [ -f app.py ]; then python app.py; else python main.py; fi"]
`

const dockerfileStatic = `FROM nginx:alpine
COPY . /usr/share/nginx/html/
EXPOSE 80
`

// dockerfileRuby drops `--deployment`, which hard-requires a committed
// Gemfile.lock and aborts without one, and copies the lockfile via a wildcard
// so its absence is not a COPY failure.
const dockerfileRuby = `FROM ruby:3.3-slim
RUN apt-get update && apt-get install -y --no-install-recommends build-essential && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY Gemfile Gemfile.loc[k] ./
RUN bundle install --jobs 4 --without development test
COPY . .
EXPOSE 3000
CMD ["sh", "-c", "if [ -f config.ru ]; then bundle exec rackup --host 0.0.0.0 --port 3000; else bundle exec ruby app.rb -o 0.0.0.0 -p 3000; fi"]
`

// dockerignoreContent keeps the build context small and — more importantly —
// keeps the repository's .git directory (which for a private repo contains the
// remote URL and can contain cached credentials) out of the produced image.
// Written alongside a generated Dockerfile only; a project that ships its own
// Dockerfile is assumed to manage its own .dockerignore.
const dockerignoreContent = `.git
.gitignore
.github
node_modules
vendor
__pycache__
*.pyc
.venv
venv
.env
.env.*
Dockerfile
.dockerignore
docker-compose.yml
*.log
`
