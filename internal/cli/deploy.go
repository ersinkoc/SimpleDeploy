package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ersinkoc/SimpleDeploy/internal/buildpack"
	compose "github.com/ersinkoc/SimpleDeploy/internal/compose"
	cfgpkg "github.com/ersinkoc/SimpleDeploy/internal/config"
	"github.com/ersinkoc/SimpleDeploy/internal/db"
	"github.com/ersinkoc/SimpleDeploy/internal/docker"
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
	if err := state.ValidateRepoURL(app.Repo); err != nil {
		return fmt.Errorf("invalid repository URL: %w", err)
	}
	app.Branch = wizard.Ask("Branch", "main")
	if err := state.ValidateBranch(app.Branch); err != nil {
		return fmt.Errorf("invalid branch: %w", err)
	}

	private := wizard.Confirm("Private repository?", false)
	if private {
		app.GitToken = wizard.AskRequired("GitHub/GitLab Token")
		encToken, err := stateEncrypt(app.GitToken)
		if err != nil {
			// Fail-closed: do not fall back to plaintext storage. The only
			// realistic failure for state.Encrypt is rand.Reader being
			// unavailable (the AES/GCM constructors can't fail here — key is
			// always 32 bytes from sha256). A working install requires the
			// host CSPRNG, so abort and let the operator fix the underlying
			// issue rather than silently persist the token in plaintext.
			return fmt.Errorf("failed to encrypt git token: %w", err)
		}
		app.GitToken = encToken
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

	if err := osMkdirAll(appDir, 0755); err != nil {
		return fmt.Errorf("failed to create app directory: %w", err)
	}

	// Everything from here to the final state save is one transaction. If any
	// step fails — or the operator declines the final confirmation — the app
	// directory must go: it holds the cloned source and a .env carrying the
	// generated database passwords, and nothing in state.json points at it, so
	// leaving it behind means orphaned credentials on disk and a stale `source`
	// tree that makes the next `deploy` of the same name fail at git clone.
	// Only the build-failure path used to clean up; every other early return
	// leaked.
	deployed := false
	defer func() {
		if !deployed {
			if err := osRemoveAll(appDir); err != nil {
				wizard.Warn(fmt.Sprintf("Failed to clean up %s after aborted deploy: %v", appDir, err))
			}
		}
	}()

	wizard.Info("Cloning repository...")
	gitToken := app.GitToken
	if private {
		decrypted, err := stateDecrypt(gitToken)
		if err == nil {
			gitToken = decrypted
		} else {
			wizard.Warn("Failed to decrypt git token: " + err.Error())
		}
	}

	if err := gitClone(context.Background(), app.Repo, app.Branch, sourceDir, gitToken); err != nil {
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
		if _, err := osStat(filepath.Join(sourceDir, "Dockerfile")); err != nil {
			wizard.Info("Generating Dockerfile for " + app.Type)
			if err := buildpackWriteDockerfile(sourceDir, app.Type); err != nil {
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
	if app.Port < 1 || app.Port > 65535 {
		wizard.Warn(fmt.Sprintf("Port %d out of range (1-65535), defaulting to 3000", app.Port))
		app.Port = 3000
	}

	// 6. Environment variables
	fmt.Println()
	envVars := wizard.AskMultiple("Environment variables (KEY=VALUE)")
	envPath := filepath.Join(appDir, ".env")

	// importedEnv holds the verbatim contents of a .env the operator asked us
	// to import. It is NOT written to disk here: the final .env is assembled
	// once, at the end of the wizard, from this content plus the generated
	// variables. Writing it early — as the previous version did — meant the
	// closing osWriteFile of generated vars silently truncated the file and
	// discarded every imported value.
	var importedEnv []byte
	if wizard.Confirm("Import an existing .env file?", false) {
		customPath := wizard.Ask(".env path", "")
		if customPath != "" {
			resolved, err := validateEnvSourcePath(customPath, envPath)
			if err != nil {
				wizard.Warn(fmt.Sprintf("Invalid .env path: %v", err))
			} else if data, err := osReadFile(resolved); err != nil {
				wizard.Warn("Could not read .env file: " + err.Error())
			} else {
				importedEnv = data
				wizard.Success(fmt.Sprintf("Imported %d bytes from %s", len(data), resolved))
			}
		}
	}

	envMap := make(map[string]string)
	for _, ev := range envVars {
		kv := strings.SplitN(ev, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		if err := state.ValidateEnvKey(key); err != nil {
			return fmt.Errorf("invalid env var: %w", err)
		}
		envMap[key] = kv[1]
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

	dbEnvVars, dbVolumes, dbCreds, err := dbProvisionDatabases(app.Name, selectedDBs)
	if err != nil {
		return fmt.Errorf("database provisioning failed: %w", err)
	}
	for k, v := range dbEnvVars {
		envMap[k] = v
	}
	app.Databases = selectedDBs
	app.DBCredentials = dbCreds

	for k, v := range app.DBCredentials {
		enc, err := stateEncrypt(v)
		if err != nil {
			// Same fail-closed rationale as the git token above: the
			// alternative is shipping the DB root password to state.json
			// in plaintext. Return early before any state is saved.
			return fmt.Errorf("failed to encrypt %s credentials: %w", k, err)
		}
		app.DBCredentials[k] = enc
	}

	// 8. Domain
	fmt.Println()
	subdomain := wizard.Ask("Subdomain", app.Name)
	if err := state.ValidateSubdomain(subdomain); err != nil {
		return fmt.Errorf("invalid subdomain: %w", err)
	}
	app.Domain = fmt.Sprintf("%s.%s", subdomain, cfg.BaseDomain)
	if err := state.ValidateAppDomain(app.Domain); err != nil {
		return fmt.Errorf("invalid app domain: %w", err)
	}

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
		if len(kv) != 2 {
			wizard.Warn(fmt.Sprintf("Ignoring malformed header %q (expected 'Name: value')", h))
			continue
		}
		name := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])
		// Validate here rather than letting compose.Generate / AddCaddyApp
		// reject it later: those run AFTER the Docker image has been built,
		// so a typo'd header name used to cost a full build before failing.
		if err := state.ValidateHeaderName(name); err != nil {
			wizard.Warn(fmt.Sprintf("Ignoring header %q: %v", name, err))
			continue
		}
		if err := state.ValidateHeaderValue(value); err != nil {
			wizard.Warn(fmt.Sprintf("Ignoring header %q: %v", name, err))
			continue
		}
		app.Headers[name] = value
	}

	// 9. Webhook
	fmt.Println()
	// Setup instructions are printed after a successful deploy (see
	// printWebhookHelp) rather than here — at this point the deploy can still
	// fail, and handing the operator a webhook URL for an app that never came
	// up is worse than saying nothing.
	app.WebhookEnabled = wizard.Confirm("Enable push-to-deploy webhook?", true)

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

	// Write .env with restricted permissions.
	if err := osWriteFile(envPath, renderEnvFile(importedEnv, envMap), 0600); err != nil {
		return fmt.Errorf("failed to write .env: %w", err)
	}

	// Build image
	wizard.Info("Building Docker image...")
	imageTag, err := dockerBuildImage(context.Background(), sourceDir, app.Name)
	if err != nil {
		// The deferred cleanup above removes the app directory (and the .env
		// holding the generated DB credentials) on this path.
		return fmt.Errorf("build failed: %w", err)
	}
	wizard.Success("Image built: " + imageTag)
	app.CurrentImage = imageTag

	// Generate compose
	composeData := buildComposeData(app, cfg, dbVolumes, envMap)
	composeContent, err := compose.Generate(composeData)
	if err != nil {
		return fmt.Errorf("failed to generate compose: %w", err)
	}
	if err := composeWriteCompose(appDir, composeContent); err != nil {
		return fmt.Errorf("failed to write compose: %w", err)
	}
	wizard.Success("Compose YAML generated")

	// Start containers
	wizard.Info("Starting containers...")
	if err := dockerComposeUp(context.Background(), appDir); err != nil {
		wizard.Warn("Failed to start containers. Rolling back...")
		// Attempt rollback: remove compose and clean up
		if downErr := dockerComposeDown(context.Background(), appDir); downErr != nil {
			wizard.Warn("Rollback cleanup also failed: " + downErr.Error())
		}
		return fmt.Errorf("failed to start containers: %w", err)
	}
	wizard.Success("Containers started")

	// Verify the container is actually running. A single check after a fixed
	// 2 s sleep raced against slow-starting images (it reported "created" for
	// anything that had not finished starting yet); poll instead so a healthy
	// app is not flagged as broken and a genuinely crashed one is still caught.
	containerName := docker.ContainerName(app.Name)
	containerStatus := waitForContainer(context.Background(), containerName, containerStartTimeout)
	if containerStatus != "running" {
		wizard.Warn(fmt.Sprintf("Container %s is %q (expected running). Check logs with 'simpledeploy logs %s'", containerName, containerStatus, app.Name))
		app.Status = "error"
	}

	// Proxy-specific post-deploy
	if cfg.Proxy == "caddy" {
		if err := proxyAddCaddyApp(app.Name, app.Domain, app.Port, app.Headers); err != nil {
			wizard.Warn("Failed to update Caddyfile: " + err.Error())
		}
		if err := proxyReloadCaddy(); err != nil {
			wizard.Warn("Failed to reload Caddy: " + err.Error())
		}
	}

	// Update state
	if app.Status != "error" {
		app.Status = "running"
	}
	app.LastDeploy = time.Now().UTC().Format(time.RFC3339)
	app.DeployCount = 1

	if err := stateSaveApp(app); err != nil {
		return fmt.Errorf("failed to save app state: %w", err)
	}
	// State now references the app directory, so the deferred cleanup must
	// not remove it. Use `simpledeploy remove` to tear the app down instead.
	deployed = true

	logDeploy(appDir, app.Name, imageTag)

	fmt.Println()
	wizard.Success(fmt.Sprintf("https://%s is ready!", app.Domain))
	if app.WebhookEnabled {
		printWebhookHelp(cfg, app.Name, app.Branch)
	}
	return nil
}

// containerStartTimeout bounds how long we wait for a freshly started
// container to report "running". Generous enough for a heavy image's
// entrypoint, short enough that a crash-looping container surfaces while the
// operator is still watching.
const containerStartTimeout = 30 * time.Second

// Container states that mean "this is not coming up" — a container that ran
// its entrypoint and stopped, or one the daemon considers unrecoverable.
// Distinguished from transient states ("created", "restarting", "paused")
// which are worth polling through, and from "not found"/lookup errors which
// mean we could not determine anything at all.
func isFailedContainerState(status string) bool {
	return status == "exited" || status == "dead"
}

// waitForContainer polls a container's state until it is "running", reaches a
// terminal failure state, or the budget expires. It returns the last state
// observed, or "unknown" if the state could never be read.
//
// Replaces a single check after a fixed 2 s sleep, which reported "created"
// for any image whose entrypoint had not finished starting and so flagged
// perfectly healthy deploys as broken.
func waitForContainer(ctx context.Context, containerName string, budget time.Duration) string {
	deadline := time.Now().Add(budget)
	for {
		status, err := dockerContainerStatus(ctx, containerName)
		switch {
		case err != nil:
			// Docker itself is unreachable. Retrying will not help and we must
			// not block the deploy on it — report "unknown" so the caller
			// treats the result as unverified rather than as a failure.
			return "unknown"
		case status == "running":
			return status
		case status == "not found":
			// compose up reported success, so the container should exist. It
			// does not — nothing to poll for.
			return status
		case isFailedContainerState(status):
			return status
		}
		if time.Now().After(deadline) {
			return status
		}
		time.Sleep(time.Second)
	}
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
			Volume:     dbCfg.Volume,
			VolumeName: volName,
			Env:        make(map[string]string),
		}

		for envKey, staticVal := range dbCfg.Env {
			switch envKey {
			case "MYSQL_ROOT_PASSWORD", "MARIADB_ROOT_PASSWORD", "POSTGRES_PASSWORD", "MONGO_INITDB_ROOT_PASSWORD":
				if cred, ok := app.DBCredentials[dbType]; ok {
					decrypted, err := stateDecrypt(cred)
					if err == nil {
						dbSvc.Env[envKey] = decrypted
					}
				}
			case "MYSQL_DATABASE", "MARIADB_DATABASE", "POSTGRES_DB":
				dbSvc.Env[envKey] = app.Name
			default:
				// Pass through any key the provisioner defines with a fixed
				// value (e.g. MONGO_INITDB_ROOT_USERNAME). Previously the
				// switch silently dropped every key it did not enumerate,
				// which is why the mongo container never received its
				// required username and refused to start. Keys with an empty
				// static value are placeholders for generated secrets and are
				// handled by the cases above, so they stay skipped here.
				if staticVal != "" {
					dbSvc.Env[envKey] = staticVal
				}
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
	f, err := osOpenFileFunc(filepath.Join(appDir, "deploy.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.WriteString(logLine); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to write deploy log: %v\n", err)
	}
}

var sanitizeNameRe = regexp.MustCompile(`[^a-z0-9-]`)

// renderEnvFile assembles the app's .env from an optional imported file plus
// the variables SimpleDeploy generated (wizard input and database connection
// strings).
//
// Ordering is load-bearing: both docker-compose and every dotenv parser apply
// assignments top-to-bottom, so the generated block goes LAST and therefore
// wins any key collision. That is the behaviour we want — a stale DATABASE_URL
// copied in from an old .env must not shadow the connection string for the
// database SimpleDeploy just provisioned.
//
// Generated keys are emitted in sorted order so redeploying an unchanged app
// produces a byte-identical file.
func renderEnvFile(imported []byte, generated map[string]string) []byte {
	var b strings.Builder

	if len(imported) > 0 {
		b.WriteString("# --- imported from operator-supplied .env ---\n")
		b.Write(imported)
		if !strings.HasSuffix(string(imported), "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if len(generated) > 0 {
		if len(imported) > 0 {
			b.WriteString("# --- generated by SimpleDeploy (overrides the above) ---\n")
		}
		keys := make([]string, 0, len(generated))
		for k := range generated {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("%s=%s\n", k, generated[k]))
		}
	}

	return []byte(b.String())
}

// validateEnvSourcePath resolves and checks the path of a .env file the
// operator asked to import.
//
// This deliberately does NOT confine the path to a base directory. The
// previous implementation required the file to live inside the app's own
// directory — which SimpleDeploy had just created moments earlier and which
// therefore never contained the operator's file — so the "I already have a
// .env" branch could not succeed for any realistic input and always fell
// through to a warning. Confinement also bought nothing: the only caller is
// the interactive deploy wizard, driven by the same root-equivalent operator
// who could `cat` the file anyway. Webhook-triggered redeploys never reach
// this code.
//
// What is still checked is that the path names a readable regular file — a
// directory or device node would otherwise produce a confusing failure much
// later — and that it is not the destination itself, which would truncate
// the file mid-copy.
func validateEnvSourcePath(customPath, destPath string) (string, error) {
	if strings.TrimSpace(customPath) == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	absPath, err := filepath.Abs(customPath)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	absPath = filepath.Clean(absPath)

	if absDest, err := filepath.Abs(destPath); err == nil {
		if filepath.Clean(absDest) == absPath {
			return "", fmt.Errorf("source and destination are the same file")
		}
	}

	info, err := osStat(absPath)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", absPath, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory, not a .env file", absPath)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", absPath)
	}

	return absPath, nil
}

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
