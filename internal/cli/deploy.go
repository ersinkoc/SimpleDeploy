package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ersinkoc/SimpleDeploy/internal/buildpack"
	compose "github.com/ersinkoc/SimpleDeploy/internal/compose"
	cfgpkg "github.com/ersinkoc/SimpleDeploy/internal/config"
	"github.com/ersinkoc/SimpleDeploy/internal/db"
	"github.com/ersinkoc/SimpleDeploy/internal/docker"
	"github.com/ersinkoc/SimpleDeploy/internal/git"
	"github.com/ersinkoc/SimpleDeploy/internal/proxy"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

func RunDeploy() error {
	cfg, err := state.GetConfig()
	if err != nil {
		return err
	}

	wizard.Header("New Application Deploy")
	app := state.NewAppConfig()

	// 1. Git Repository
	fmt.Println()
	app.Repo = wizard.AskRequired("Git Repository URL")
	app.Branch = wizard.Ask("Branch", "main")

	private := wizard.Confirm("Private repository?", false)
	if private {
		app.GitToken = wizard.AskRequired("GitHub/GitLab Token")
		encToken, err := state.Encrypt(app.GitToken)
		if err != nil {
			wizard.Warn("Failed to encrypt token, storing as plaintext")
		} else {
			app.GitToken = encToken
		}
	}

	// 2. App name
	parts := strings.Split(app.Repo, "/")
	defaultName := strings.TrimSuffix(parts[len(parts)-1], ".git")
	defaultName = sanitizeDefaultName(defaultName)
	app.Name = wizard.Ask("Application name", defaultName)

	if err := state.ValidateAppName(app.Name); err != nil {
		return err
	}

	if existing, _ := state.GetApp(app.Name); existing != nil {
		return fmt.Errorf("application '%s' already exists. Use 'simpledeploy redeploy %s' to update", app.Name, app.Name)
	}

	// 3. Clone repo
	appDir := cfgpkg.AppDir(app.Name)
	sourceDir := filepath.Join(appDir, "source")

	if err := os.MkdirAll(appDir, 0755); err != nil {
		return fmt.Errorf("failed to create app directory: %w", err)
	}

	wizard.Info("Cloning repository...")
	gitToken := app.GitToken
	if private {
		decrypted, err := state.Decrypt(gitToken)
		if err == nil {
			gitToken = decrypted
		} else {
			wizard.Warn("Failed to decrypt git token: " + err.Error())
		}
	}

	if err := git.Clone(app.Repo, app.Branch, sourceDir, gitToken); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}
	wizard.Success("Repository cloned")

	// 4. Detect app type
	fmt.Println()
	detected := buildpack.Detect(sourceDir)
	appTypes := []string{
		"Node.js", "Go", "PHP", "Python", "Ruby",
		"Static (HTML/CSS/JS)", "Dockerfile (use existing)",
	}

	if detected.Detected {
		wizard.Info(fmt.Sprintf("Detected: %s", detected.Name))
	}

	choice := wizard.Choose("Application type:", appTypes, mapDetectedDefault(detected.Name))
	switch choice {
	case 1:
		app.Type = buildpack.TypeNode
	case 2:
		app.Type = buildpack.TypeGo
	case 3:
		app.Type = buildpack.TypePHP
	case 4:
		app.Type = buildpack.TypePython
	case 5:
		app.Type = buildpack.TypeRuby
	case 6:
		app.Type = buildpack.TypeStatic
	case 7:
		app.Type = buildpack.TypeDocker
	}

	if app.Type != buildpack.TypeDocker {
		if _, err := os.Stat(filepath.Join(sourceDir, "Dockerfile")); err != nil {
			wizard.Info("Generating Dockerfile for " + app.Type)
			if err := buildpack.WriteDockerfile(sourceDir, app.Type); err != nil {
				return fmt.Errorf("failed to generate Dockerfile: %w", err)
			}
		}
	}

	// 5. Port
	fmt.Println()
	portStr := wizard.Ask("Application port", strconv.Itoa(detected.Port))
	app.Port, err = strconv.Atoi(portStr)
	if err != nil {
		wizard.Warn(fmt.Sprintf("Invalid port %q, defaulting to 3000", portStr))
		app.Port = 3000
	}
	if app.Port == 0 {
		app.Port = 3000
	}

	// 6. Environment variables
	fmt.Println()
	envVars := wizard.AskMultiple("Environment variables (KEY=VALUE)")
	envPath := filepath.Join(appDir, ".env")
	if wizard.Confirm(".env file exists?", false) {
		customPath := wizard.Ask(".env path", "")
		if customPath != "" {
			data, err := os.ReadFile(customPath)
			if err != nil {
				wizard.Warn("Could not read .env file: " + err.Error())
			} else if err := os.WriteFile(envPath, data, 0600); err != nil {
				wizard.Warn("Failed to write .env: " + err.Error())
			}
		}
	}

	envMap := make(map[string]string)
	for _, ev := range envVars {
		kv := strings.SplitN(ev, "=", 2)
		if len(kv) == 2 {
			envMap[kv[0]] = kv[1]
		}
	}

	// 7. Databases
	fmt.Println()
	availableDBs := db.AvailableDatabases()
	dbOptions := make([]string, 0, len(availableDBs)+1)
	for _, name := range availableDBs {
		if cfg, ok := db.GetDatabaseConfig(name); ok {
			imgParts := strings.SplitN(cfg.Image, ":", 2)
			label := strings.ToUpper(string(name[0])) + name[1:]
			if len(imgParts) == 2 {
				label += " " + imgParts[1]
			}
			dbOptions = append(dbOptions, label)
		}
	}
	dbOptions = append(dbOptions, "None")
	dbChoices := wizard.MultiChoose("Database requirements:", dbOptions)

	var selectedDBs []string
	dbMap := make(map[int]string)
	for i, name := range availableDBs {
		dbMap[i+1] = name
	}
	for _, c := range dbChoices {
		if dbType, ok := dbMap[c]; ok {
			selectedDBs = append(selectedDBs, dbType)
		}
	}

	dbEnvVars, dbVolumes, dbCreds, err := db.ProvisionDatabases(app.Name, selectedDBs)
	if err != nil {
		return fmt.Errorf("database provisioning failed: %w", err)
	}
	for k, v := range dbEnvVars {
		envMap[k] = v
	}
	app.Databases = selectedDBs
	app.DBCredentials = dbCreds

	for k, v := range app.DBCredentials {
		enc, err := state.Encrypt(v)
		if err != nil {
			wizard.Warn(fmt.Sprintf("Failed to encrypt %s credentials, storing as plaintext", k))
		} else {
			app.DBCredentials[k] = enc
		}
	}

	// 8. Domain
	fmt.Println()
	subdomain := wizard.Ask("Subdomain", app.Name)
	app.Domain = fmt.Sprintf("%s.%s", subdomain, cfg.BaseDomain)

	app.Headers = map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "SAMEORIGIN",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
		"X-XSS-Protection":       "1; mode=block",
	}

	fmt.Println()
	extraHeaders := wizard.AskMultiple("Extra headers (Header-Name: value)")
	for _, h := range extraHeaders {
		kv := strings.SplitN(h, ":", 2)
		if len(kv) == 2 {
			app.Headers[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}

	// 9. Webhook
	fmt.Println()
	app.WebhookEnabled = wizard.Confirm("Enable GitHub webhook auto-deploy?", true)
	if app.WebhookEnabled {
		webhookURL := fmt.Sprintf("https://%s/_qd/webhook/%s", cfg.BaseDomain, app.Name)
		wizard.Info("Webhook URL: " + webhookURL)
		wizard.Info("Add this URL to your repository Settings -> Webhooks")
		wizard.Info("Event: push (branch: " + app.Branch + ")")
	}

	// Summary
	fmt.Println()
	wizard.Header("Summary")
	fmt.Printf("  App:      %s\n", app.Name)
	fmt.Printf("  Repo:     %s\n", app.Repo)
	fmt.Printf("  Type:     %s\n", app.Type)
	fmt.Printf("  Domain:   %s (SSL enabled)\n", app.Domain)
	fmt.Printf("  Port:     %d\n", app.Port)
	if len(app.Databases) > 0 {
		fmt.Printf("  DB:       %s\n", strings.Join(app.Databases, ", "))
	}
	fmt.Printf("  Webhook:  %v\n", app.WebhookEnabled)

	fmt.Println()
	if !wizard.Confirm("Start deployment?", true) {
		return nil
	}

	// Write .env with restricted permissions
	var envLines []string
	for k, v := range envMap {
		envLines = append(envLines, fmt.Sprintf("%s=%s", k, v))
	}
	if err := os.WriteFile(envPath, []byte(strings.Join(envLines, "\n")), 0600); err != nil {
		return fmt.Errorf("failed to write .env: %w", err)
	}

	// Build image
	wizard.Info("Building Docker image...")
	imageTag, err := docker.BuildImage(sourceDir, app.Name)
	if err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	wizard.Success("Image built: " + imageTag)
	app.CurrentImage = imageTag

	// Generate compose
	composeData := buildComposeData(app, cfg, dbVolumes, envMap)
	composeContent := compose.Generate(composeData)
	if err := compose.WriteCompose(appDir, composeContent); err != nil {
		return fmt.Errorf("failed to write compose: %w", err)
	}
	wizard.Success("Compose YAML generated")

	// Start containers
	wizard.Info("Starting containers...")
	if err := docker.ComposeUp(appDir); err != nil {
		wizard.Warn("Failed to start containers. Rolling back...")
		// Attempt rollback: remove compose and clean up
		if downErr := docker.ComposeDown(appDir); downErr != nil {
			wizard.Warn("Rollback cleanup also failed: " + downErr.Error())
		}
		return fmt.Errorf("failed to start containers: %w", err)
	}
	wizard.Success("Containers started")

	// Verify container is actually running
	containerName := "qd-" + app.Name
	time.Sleep(2 * time.Second)
	containerStatus, _ := docker.ContainerStatus(containerName)
	if containerStatus != "running" {
		wizard.Warn(fmt.Sprintf("Container %s is %q (expected running). Check logs with 'simpledeploy logs %s'", containerName, containerStatus, app.Name))
		app.Status = "error"
	}

	// Proxy-specific post-deploy
	if cfg.Proxy == "caddy" {
		if err := proxy.AddCaddyApp(app.Name, app.Domain, app.Port, app.Headers); err != nil {
			wizard.Warn("Failed to update Caddyfile: " + err.Error())
		}
		if err := proxy.ReloadCaddy(); err != nil {
			wizard.Warn("Failed to reload Caddy: " + err.Error())
		}
	}

	// Update state
	app.Status = "running"
	app.LastDeploy = time.Now().UTC().Format(time.RFC3339)
	app.DeployCount = 1

	if err := state.SaveApp(app); err != nil {
		return fmt.Errorf("failed to save app state: %w", err)
	}

	logDeploy(appDir, app.Name, imageTag)

	fmt.Println()
	wizard.Success(fmt.Sprintf("https://%s is ready!", app.Domain))
	return nil
}

func mapDetectedDefault(appType string) int {
	switch appType {
	case buildpack.TypeNode:
		return 1
	case buildpack.TypeGo:
		return 2
	case buildpack.TypePHP:
		return 3
	case buildpack.TypePython:
		return 4
	case buildpack.TypeDocker:
		return 7
	default:
		return 7
	}
}

func buildComposeData(app *state.AppConfig, cfg *state.GlobalConfig, volumes []string, envVars map[string]string) *compose.ComposeData {
	data := compose.NewComposeData(app, cfg)
	data.Environment = envVars

	for i, dbType := range app.Databases {
		dbCfg, ok := db.GetDatabaseConfig(dbType)
		if !ok {
			continue
		}

		volName := ""
		if i < len(volumes) {
			volName = volumes[i]
		}
		dbSvc := compose.DBService{
			Type:       dbType,
			Image:      dbCfg.Image,
			Container:  dbCfg.Container,
			Volume:     dbCfg.Volume,
			VolumeName: volName,
			Env:        make(map[string]string),
		}

		for envKey := range dbCfg.Env {
			switch envKey {
			case "MYSQL_ROOT_PASSWORD", "MARIADB_ROOT_PASSWORD", "POSTGRES_PASSWORD", "MONGO_INITDB_ROOT_PASSWORD":
				if cred, ok := app.DBCredentials[dbType]; ok {
					decrypted, err := state.Decrypt(cred)
					if err == nil {
						dbSvc.Env[envKey] = decrypted
					}
				}
			case "MYSQL_DATABASE", "MARIADB_DATABASE", "POSTGRES_DB":
				dbSvc.Env[envKey] = app.Name
			}
		}

		if dbCfg.HealthCheck != nil {
			testSlice := []string{}
			if t, ok := dbCfg.HealthCheck["test"]; ok {
				if arr, ok := t.([]string); ok {
					testSlice = arr
				}
			}
			interval := "10s"
			if v, ok := dbCfg.HealthCheck["interval"]; ok {
				if s, ok := v.(string); ok {
					interval = s
				}
			}
			timeout := "5s"
			if v, ok := dbCfg.HealthCheck["timeout"]; ok {
				if s, ok := v.(string); ok {
					timeout = s
				}
			}
			retries := 5
			if v, ok := dbCfg.HealthCheck["retries"]; ok {
				if n, ok := v.(int); ok {
					retries = n
				}
			}
			dbSvc.HealthCheck = &compose.HealthCheckData{
				Test:     testSlice,
				Interval: interval,
				Timeout:  timeout,
				Retries:  retries,
			}
		}

		data.Databases = append(data.Databases, dbSvc)
	}

	return data
}

func logDeploy(appDir, appName, imageTag string) {
	logLine := fmt.Sprintf("[%s] Deployed %s with image %s\n",
		time.Now().UTC().Format(time.RFC3339), appName, imageTag)
	f, err := os.OpenFile(filepath.Join(appDir, "deploy.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.WriteString(logLine); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to write deploy log: %v\n", err)
	}
}

var sanitizeNameRe = regexp.MustCompile(`[^a-z0-9-]`)

// sanitizeDefaultName converts a repo-derived name into a safe default app name.
func sanitizeDefaultName(name string) string {
	name = strings.ToLower(name)
	name = sanitizeNameRe.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if len(name) > 63 {
		name = name[:63]
	}
	if name == "" {
		name = "app"
	}
	return name
}
