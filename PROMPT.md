# SimpleDeploy — Single-Binary PaaS CLI in Go

## Project Overview

**SimpleDeploy** — A single Go binary, interactive CLI-based, zero external dependency PaaS tool. Users provide a Git repo URL, and SimpleDeploy handles the rest: Docker installation, image build, database provisioning, reverse proxy (Traefik or Caddy), SSL, and auto-deploy via webhooks. It also runs as a Docker container service.

**Philosophy:** `#NOFORKANYMORE` — Do what Coolify/Dokploy does with 1000 lines of Go. Generate docker-compose.yml, that's it.

---

## Technical Requirements

- **Language:** Go 1.22+
- **External dependencies:** ZERO (only Go stdlib, Docker invoked via exec)
- **Build:** Single binary, `CGO_ENABLED=0`, statically linked
- **Data:** Simple JSON file (`~/.simpledeploy/state.json`) instead of embedded SQLite
- **Config:** `~/.simpledeploy/config.json`

---

## Architecture

```
simpledeploy (single binary)
├── cmd/             → CLI entry point (main.go)
├── internal/
│   ├── wizard/      → Interactive setup wizard (questions, input)
│   ├── git/         → Git clone/pull (exec: git CLI)
│   ├── docker/      → Docker client (install, build, compose, run)
│   ├── compose/     → docker-compose.yml generator (template engine)
│   ├── proxy/       → Traefik / Caddy config generator
│   ├── webhook/     → HTTP webhook server (GitHub/GitLab/Gitea)
│   ├── ssl/         → Let's Encrypt integration (Traefik/Caddy automatic)
│   ├── db/          → Database provisioning (MySQL, PostgreSQL, Redis, MongoDB)
│   ├── state/       → JSON-based state management
│   └── runner/      → Service mode (daemonize, health check)
└── templates/       → Embedded compose/proxy templates (embed.FS)
```

---

## CLI Commands

```bash
# First-time setup — interactive wizard
simpledeploy init

# Deploy a new application — interactive
simpledeploy deploy

# List deployed applications
simpledeploy list

# Update an application (manual redeploy)
simpledeploy redeploy <app-name>

# Remove an application
simpledeploy remove <app-name>

# Show application logs
simpledeploy logs <app-name>

# Install SimpleDeploy itself as a service (Docker container)
simpledeploy service install
simpledeploy service start
simpledeploy service stop

# Start webhook server (automatic in service mode)
simpledeploy webhook start --port 9000

# Show status
simpledeploy status
```

---

## `simpledeploy init` Wizard Flow

Interactive questions on first run:

```
🚀 SimpleDeploy Setup
═══════════════════════

1. Checking if Docker is installed...
   ✗ Docker not found. Install it? [Y/n]: Y
   → Running Docker Engine install script
   → Checking / installing docker compose plugin
   ✓ Docker 27.x ready

2. Reverse Proxy selection:
   [1] Traefik (recommended, auto-discovery)
   [2] Caddy (simple, auto-SSL)
   Selection [1]: 1

3. Domain/IP information:
   Base domain (e.g.: example.com): apps.ersin.dev
   Wildcard DNS configured? (*.apps.ersin.dev → this server) [Y/n]: Y

4. SSL:
   Let's Encrypt email: ersin@ecostack.dev

5. Webhook secret (GitHub/GitLab webhook verification):
   Auto-generate? [Y/n]: Y
   → Secret: whk_a8f3b2c1...

✓ Reverse proxy started (Traefik)
✓ Webhook server ready on port :9000
✓ Config: ~/.simpledeploy/config.json
```

---

## `simpledeploy deploy` Wizard Flow

For each new application:

```
📦 New Application Deploy
═══════════════════════════

1. Git Repository:
   URL: https://github.com/user/myapp.git
   Branch [main]: main
   Private repo? [y/N]: Y
   → GitHub Token: ghp_xxxx...

2. Application name [myapp]: myapp

3. Application type (auto-detected, confirmation requested):
   Detected: Node.js (package.json found)
   [1] Node.js     [2] Go      [3] PHP
   [4] Python      [5] Ruby    [6] Static
   [7] Dockerfile exists, use it
   Selection [7]: 7

4. Port:
   Which port does the application listen on? [3000]: 3000

5. Environment Variables:
   .env file exists? [y/N]: Y
   → .env path: /path/to/.env
   Extra variables? (KEY=VALUE, leave empty to finish):
   > DATABASE_URL=mysql://root:secret@myapp-db:3306/myapp
   > REDIS_URL=redis://myapp-redis:6379
   >

6. Database requirements:
   [1] MySQL 8
   [2] PostgreSQL 16
   [3] MariaDB 11
   [4] MongoDB 7
   [5] Redis 7
   [6] None
   [0] Select multiple (comma-separated: 1,5)
   Selection: 1,5

   MySQL root password [auto-generate]:
   → Password: db_x9f2k4m1
   MySQL database name [myapp]: myapp

   Redis password [none]:

7. Domain:
   Subdomain [myapp]: myapp
   → myapp.apps.ersin.dev

   Add extra headers?
   > X-Frame-Options: DENY
   > X-Content-Type-Options: nosniff
   >

8. Webhook:
   GitHub webhook auto-deploy? [Y/n]: Y
   → Webhook URL: https://apps.ersin.dev/_qd/webhook/myapp
   → Add this URL to GitHub repo Settings → Webhooks
   → Event: push (branch: main)

═══════════════════════════

Summary:
  App:      myapp
  Repo:     https://github.com/user/myapp.git
  Type:     Dockerfile
  Domain:   myapp.apps.ersin.dev (SSL ✓)
  Port:     3000
  DB:       MySQL 8 + Redis 7
  Webhook:  Active
  Headers:  X-Frame-Options, X-Content-Type-Options

Start deployment? [Y/n]: Y

→ Git clone... ✓
→ Docker build... ✓  (myapp:20240115-abc123)
→ MySQL container... ✓
→ Redis container... ✓
→ Compose YAML generated... ✓
→ docker compose up -d... ✓
→ Waiting for Traefik discovery... ✓
→ SSL certificate obtained... ✓

✅ https://myapp.apps.ersin.dev is ready!
```

---

## Compose YAML Generation — Core Mechanism

This is the heart of the entire system. `internal/compose/generator.go`:

### Example generated docker-compose.yml:

```yaml
# Auto-generated by SimpleDeploy — DO NOT EDIT
# App: myapp | Generated: 2024-01-15T10:30:00Z

services:
  myapp:
    image: myapp:20240115-abc123
    container_name: qd-myapp
    restart: unless-stopped
    networks:
      - simpledeploy
    ports:
      - "3000"
    env_file:
      - /opt/simpledeploy/apps/myapp/.env
    environment:
      - DATABASE_URL=mysql://root:db_x9f2k4m1@qd-myapp-mysql:3306/myapp
      - REDIS_URL=redis://qd-myapp-redis:6379
    depends_on:
      qd-myapp-mysql:
        condition: service_healthy
      qd-myapp-redis:
        condition: service_started
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.myapp.rule=Host(`myapp.apps.ersin.dev`)"
      - "traefik.http.routers.myapp.entrypoints=websecure"
      - "traefik.http.routers.myapp.tls.certresolver=letsencrypt"
      - "traefik.http.services.myapp.loadbalancer.server.port=3000"
      # Security headers
      - "traefik.http.middlewares.myapp-headers.headers.customresponseheaders.X-Frame-Options=DENY"
      - "traefik.http.middlewares.myapp-headers.headers.customresponseheaders.X-Content-Type-Options=nosniff"
      - "traefik.http.routers.myapp.middlewares=myapp-headers"
      # SimpleDeploy metadata
      - "simpledeploy.managed=true"
      - "simpledeploy.app=myapp"
      - "simpledeploy.repo=https://github.com/user/myapp.git"
      - "simpledeploy.branch=main"

  qd-myapp-mysql:
    image: mysql:8
    container_name: qd-myapp-mysql
    restart: unless-stopped
    networks:
      - simpledeploy
    environment:
      MYSQL_ROOT_PASSWORD: db_x9f2k4m1
      MYSQL_DATABASE: myapp
    volumes:
      - qd-myapp-mysql-data:/var/lib/mysql
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "localhost"]
      interval: 10s
      timeout: 5s
      retries: 5

  qd-myapp-redis:
    image: redis:7-alpine
    container_name: qd-myapp-redis
    restart: unless-stopped
    networks:
      - simpledeploy
    volumes:
      - qd-myapp-redis-data:/data

volumes:
  qd-myapp-mysql-data:
  qd-myapp-redis-data:
```

---

## Traefik Reverse Proxy Setup

Traefik compose created during `simpledeploy init`:

```yaml
# /opt/simpledeploy/proxy/docker-compose.yml
networks:
  simpledeploy:
    name: simpledeploy

services:
  traefik:
    image: traefik:v3
    container_name: qd-traefik
    restart: unless-stopped
    command:
      - "--api.dashboard=false"
      - "--providers.docker=true"
      - "--providers.docker.exposedbydefault=false"
      - "--providers.docker.network=simpledeploy"
      - "--entrypoints.web.address=:80"
      - "--entrypoints.websecure.address=:443"
      - "--entrypoints.web.http.redirections.entryPoint.to=websecure"
      - "--certificatesresolvers.letsencrypt.acme.email=${ACME_EMAIL}"
      - "--certificatesresolvers.letsencrypt.acme.storage=/letsencrypt/acme.json"
      - "--certificatesresolvers.letsencrypt.acme.httpchallenge.entrypoint=web"
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - qd-letsencrypt:/letsencrypt
    networks:
      - simpledeploy

volumes:
  qd-letsencrypt:
```

---

## Webhook Server

`internal/webhook/server.go` — Simple HTTP server:

```
POST /_qd/webhook/:app-name

1. Verify GitHub signature (HMAC-SHA256 + webhook secret)
2. Check branch (only configured branch)
3. Check event type (only "push")
4. Trigger redeploy:
   a. git pull
   b. docker build (new tag: timestamp-shortsha)
   c. Update compose YAML (new image tag)
   d. docker compose up -d (zero-downtime: depends_on order)
   e. Clean old images (keep last 3)
5. Log deployment
```

The webhook server itself runs behind Traefik:
```yaml
labels:
  - "traefik.enable=true"
  - "traefik.http.routers.qd-webhook.rule=Host(`apps.ersin.dev`) && PathPrefix(`/_qd`)"
  - "traefik.http.routers.qd-webhook.entrypoints=websecure"
  - "traefik.http.routers.qd-webhook.tls.certresolver=letsencrypt"
  - "traefik.http.services.qd-webhook.loadbalancer.server.port=9000"
```

---

## Service Mode (Self-Containerization)

`simpledeploy service install` command:

```yaml
# /opt/simpledeploy/service/docker-compose.yml
services:
  simpledeploy:
    image: simpledeploy:latest
    container_name: qd-service
    restart: unless-stopped
    command: ["webhook", "start", "--port", "9000"]
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - /opt/simpledeploy:/opt/simpledeploy
      - /usr/bin/docker:/usr/bin/docker:ro
      - /usr/local/bin/docker-compose:/usr/local/bin/docker-compose:ro
    networks:
      - simpledeploy
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.qd-api.rule=Host(`${BASE_DOMAIN}`) && PathPrefix(`/_qd`)"
      - "traefik.http.routers.qd-api.entrypoints=websecure"
      - "traefik.http.routers.qd-api.tls.certresolver=letsencrypt"
      - "traefik.http.services.qd-api.loadbalancer.server.port=9000"
```

---

## Runtime File Structure

```
/opt/simpledeploy/
├── config.json              → Global config
├── state.json               → All apps' state
├── proxy/
│   └── docker-compose.yml   → Traefik/Caddy compose
├── apps/
│   ├── myapp/
│   │   ├── source/           → Git clone
│   │   ├── docker-compose.yml → Generated compose
│   │   ├── .env              → Environment variables
│   │   └── deploy.log        → Deploy history
│   └── another-app/
│       └── ...
└── service/
    └── docker-compose.yml    → SimpleDeploy's own compose
```

---

## State JSON Structure

```json
{
  "version": 1,
  "apps": {
    "myapp": {
      "name": "myapp",
      "repo": "https://github.com/user/myapp.git",
      "branch": "main",
      "domain": "myapp.apps.ersin.dev",
      "port": 3000,
      "type": "dockerfile",
      "current_image": "myapp:20240115-abc123",
      "databases": ["mysql", "redis"],
      "webhook_enabled": true,
      "headers": {
        "X-Frame-Options": "DENY",
        "X-Content-Type-Options": "nosniff"
      },
      "created_at": "2024-01-15T10:30:00Z",
      "last_deploy": "2024-01-15T10:35:00Z",
      "deploy_count": 1,
      "status": "running"
    }
  },
  "config": {
    "base_domain": "apps.ersin.dev",
    "proxy": "traefik",
    "acme_email": "ersin@ecostack.dev",
    "webhook_port": 9000,
    "webhook_secret": "whk_a8f3b2c1..."
  }
}
```

---

## Docker Automatic Installation

`internal/docker/installer.go`:

```
1. Run docker --version
2. If not found:
   a. Detect OS (Ubuntu/Debian/CentOS/Fedora/Alpine)
   b. curl -fsSL https://get.docker.com | sh
   c. systemctl enable --now docker
   d. Add current user to docker group
3. Check docker compose version
4. If not found:
   a. Install Docker Compose v2 plugin
5. docker network create simpledeploy (if not exists)
```

---

## Buildpack Support (When No Dockerfile Exists)

If no Dockerfile exists in the repo, auto-generate one based on application type:

### Node.js
```dockerfile
FROM node:22-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci --production
COPY . .
RUN npm run build 2>/dev/null || true

FROM node:22-alpine
WORKDIR /app
COPY --from=builder /app .
EXPOSE 3000
CMD ["node", "index.js"]
```

### Go
```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app/server .

FROM alpine:3.19
COPY --from=builder /app/server /server
EXPOSE 8080
CMD ["/server"]
```

### PHP
```dockerfile
FROM php:8.3-apache
COPY . /var/www/html/
RUN chown -R www-data:www-data /var/www/html
EXPOSE 80
```

### Python
```dockerfile
FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE 8000
CMD ["python", "app.py"]
```

### Static (HTML/CSS/JS)
```dockerfile
FROM nginx:alpine
COPY . /usr/share/nginx/html/
EXPOSE 80
```

---

## Caddy Alternative

If the user selects Caddy, it is installed instead of Traefik. Caddy doesn't support labels, so a Caddyfile is generated instead:

```
# /opt/simpledeploy/proxy/Caddyfile
{
    email ersin@ecostack.dev
}

myapp.apps.ersin.dev {
    reverse_proxy qd-myapp:3000
    header X-Frame-Options "DENY"
    header X-Content-Type-Options "nosniff"
}
```

On each deploy, regenerate Caddyfile + `docker exec qd-caddy caddy reload`.

---

## Security Requirements

1. **Git tokens** → Store encrypted in state.json (AES-256-GCM, machine-id key)
2. **DB passwords** → Generate with crypto/rand, store encrypted in state.json
3. **Webhook secret** → HMAC-SHA256 verification mandatory
4. **Docker socket** → Access only from SimpleDeploy service container
5. **Default headers** → Automatically add security headers to every app:
   - `X-Content-Type-Options: nosniff`
   - `X-Frame-Options: SAMEORIGIN`
   - `Referrer-Policy: strict-origin-when-cross-origin`
   - `X-XSS-Protection: 1; mode=block`

---

## Coding Rules

1. **Zero external dependencies** — Only Go stdlib. Use `os/exec` to call Docker CLI
2. Prefer invoking Docker CLI via `os/exec` so there isn't even a single dependency
3. **Embedded templates** — Embed compose/Dockerfile templates in binary with `embed.FS`
4. **Error handling** — Every error must be shown to the user with a clear message
5. **Colored output** — Use ANSI escape codes (DO NOT use 3rd party libraries)
6. **Interactive input** — Use `bufio.Scanner` + `fmt.Print` (DO NOT use 3rd party prompt libraries)
7. **Idempotent** — Every command should be re-runnable, checking current state first
8. **Zero-downtime deploy** — Start new container, pass health check, remove old one

---

## Target Usage Scenario

```bash
# SSH into server
ssh root@server

# Install with a single command
curl -fsSL https://simpledeploy.dev/install | sh

# First-time setup
simpledeploy init

# First application
simpledeploy deploy
# → Answer questions, live in 2 minutes

# Second application
simpledeploy deploy
# → Same server, behind the same Traefik

# Automatic via webhook
# Push to GitHub → auto redeploy
```

---

## File List (To Be Created)

```
simpledeploy/
├── main.go
├── go.mod
├── internal/
│   ├── cli/
│   │   ├── root.go          → Command router
│   │   ├── init.go           → init command
│   │   ├── deploy.go         → deploy command
│   │   ├── list.go           → list command
│   │   ├── redeploy.go       → redeploy command
│   │   ├── remove.go         → remove command
│   │   ├── logs.go           → logs command
│   │   ├── status.go         → status command
│   │   └── service.go        → service install/start/stop
│   ├── wizard/
│   │   ├── prompt.go         → Input helpers (ask, choose, confirm)
│   │   └── colors.go         → ANSI color helpers
│   ├── git/
│   │   └── git.go            → Clone, pull, branch ops
│   ├── docker/
│   │   ├── installer.go      → Docker installation
│   │   ├── builder.go        → Image build
│   │   └── runner.go         → Container lifecycle
│   ├── compose/
│   │   ├── generator.go      → YAML producer
│   │   └── templates.go      → Embedded templates
│   ├── proxy/
│   │   ├── traefik.go        → Traefik setup + config
│   │   └── caddy.go          → Caddy setup + config
│   ├── webhook/
│   │   ├── server.go         → HTTP webhook handler
│   │   └── github.go         → GitHub signature verify
│   ├── db/
│   │   └── provisioner.go    → DB container setup
│   ├── state/
│   │   ├── state.go          → JSON state CRUD
│   │   └── crypto.go         → AES encryption for secrets
│   ├── buildpack/
│   │   └── detect.go         → Auto-detect app type + generate Dockerfile
│   └── runner/
│       └── service.go        → Self-containerize
└── templates/
    ├── compose.yml.tmpl      → Main compose template
    ├── traefik.yml.tmpl      → Traefik compose template
    ├── caddy.yml.tmpl        → Caddy compose template
    ├── Dockerfile.node.tmpl
    ├── Dockerfile.go.tmpl
    ├── Dockerfile.php.tmpl
    ├── Dockerfile.python.tmpl
    └── Dockerfile.static.tmpl
```

---

## Important Notes

- Use `encoding/json` and `text/template` for YAML generation. DO NOT use 3rd party YAML libraries. Template output should be valid YAML.
- Use `os.Args` + simple switch/case for CLI argument parsing. DO NOT use Cobra, urfave/cli or similar frameworks.
- Use `net/http` stdlib for the HTTP webhook server.
- All compose files must carry the `# Auto-generated by SimpleDeploy` header.
- Each app has its own compose file. There is NO global compose. This allows per-app up/down operations.
- Container naming: `qd-{appname}`, `qd-{appname}-mysql`, `qd-{appname}-redis`
- Network: All containers are on the `simpledeploy` network. Traefik listens on this network.
