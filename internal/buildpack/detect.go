package buildpack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type AppType struct {
	Name        string
	Detected    bool
	Port        int
	Entrypoint  string
}

const (
	TypeNode    = "node"
	TypeGo      = "go"
	TypePHP     = "php"
	TypePython  = "python"
	TypeRuby    = "ruby"
	TypeStatic  = "static"
	TypeDocker  = "dockerfile"
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
	// Read package.json for start script hints
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
	if strings.Contains(content, `"port"`) {
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
	return os.WriteFile(filepath.Join(repoDir, "Dockerfile"), []byte(template), 0644)
}

const dockerfileNode = `FROM node:22-alpine AS builder
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
`

const dockerfileGo = `FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app/server .

FROM alpine:3.19
COPY --from=builder /app/server /server
EXPOSE 8080
CMD ["/server"]
`

const dockerfilePHP = `FROM php:8.3-apache
COPY . /var/www/html/
RUN chown -R www-data:www-data /var/www/html
EXPOSE 80
`

const dockerfilePython = `FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE 8000
CMD ["python", "app.py"]
`

const dockerfileStatic = `FROM nginx:alpine
COPY . /usr/share/nginx/html/
EXPOSE 80
`

const dockerfileRuby = `FROM ruby:3.3-slim
WORKDIR /app
COPY Gemfile Gemfile.lock ./
RUN bundle install --jobs 4 --deployment --without development test
COPY . .
EXPOSE 3000
CMD ["bundle", "exec", "rackup", "--host", "0.0.0.0", "--port", "3000"]
`
