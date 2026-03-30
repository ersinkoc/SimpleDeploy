package cli

import (
	"io"
	"os"

	"github.com/ersinkoc/SimpleDeploy/internal/buildpack"
	compose "github.com/ersinkoc/SimpleDeploy/internal/compose"
	"github.com/ersinkoc/SimpleDeploy/internal/db"
	"github.com/ersinkoc/SimpleDeploy/internal/docker"
	"github.com/ersinkoc/SimpleDeploy/internal/git"
	"github.com/ersinkoc/SimpleDeploy/internal/proxy"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
	"github.com/ersinkoc/SimpleDeploy/internal/webhook"
)

type webhookServer interface {
	SetDeployHandler(func(appName string) error)
	Start() error
}

var webhookNewServer = func(port int, secret string) webhookServer {
	return webhook.NewServer(port, secret)
}

// Injectable dependencies for testability.
var (
	osMkdirAll             = os.MkdirAll
	osRemoveAll            = os.RemoveAll
	osStat                 = os.Stat
	osReadFile             = os.ReadFile
	osWriteFile            = os.WriteFile
	osUserHomeDir          = os.UserHomeDir
	osOpenFile             = os.OpenFile
	gitClone               = git.Clone
	gitPull                = git.Pull
	dockerBuildImage       = docker.BuildImage
	dockerComposeUp        = docker.ComposeUp
	dockerComposeDown      = docker.ComposeDown
	dockerContainerStatus  = docker.ContainerStatus
	dockerRestartContainer = docker.RestartContainer
	dockerStopContainer    = docker.StopContainer
	dockerExecContainer    = docker.ExecContainer
	dockerComposeLogs      = docker.ComposeLogs
	dockerCleanupOldImages = docker.CleanupOldImages
	dockerComposeRemove    = docker.ComposeRemove
	buildpackWriteDockerfile = buildpack.WriteDockerfile
	composeWriteCompose    = compose.WriteCompose
	dbProvisionDatabases   = db.ProvisionDatabases
	dockerEnsureDocker     = docker.EnsureDocker
	proxySetupTraefik      = proxy.SetupTraefik
	proxySetupCaddy        = proxy.SetupCaddy
	proxyAddCaddyApp       = proxy.AddCaddyApp
	proxyReloadCaddy       = proxy.ReloadCaddy
	proxyRemoveCaddyApp    = proxy.RemoveCaddyApp
	stateGetConfig         = state.GetConfig
	stateSaveConfig        = state.SaveConfig
	stateGetApp            = state.GetApp
	stateSaveApp           = state.SaveApp
	stateRemoveApp         = state.RemoveApp
	stateLoad              = state.Load
	stateEncrypt           = state.Encrypt
	stateDecrypt           = state.Decrypt
	stateGenerateSecret    = state.GenerateSecret
)

type fileWriter interface {
	WriteString(s string) (int, error)
	Close() error
}

var osOpenFileFunc = func(name string, flag int, perm os.FileMode) (fileWriter, error) {
	f, err := osOpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// discardWriter drops all writes but never errors.
type discardWriter struct{}

func (d *discardWriter) WriteString(s string) (int, error) { return len(s), nil }
func (d *discardWriter) Close() error                      { return nil }

// failWriter always returns an error on WriteString.
type failWriter struct{}

func (f *failWriter) WriteString(s string) (int, error) { return 0, io.ErrShortWrite }
func (f *failWriter) Close() error                      { return nil }
