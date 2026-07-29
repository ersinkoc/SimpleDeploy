package docker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

var (
	execLookPath  = exec.LookPath
	wizardConfirm = wizard.Confirm
)

func IsInstalled() bool {
	_, err := execLookPath("docker")
	return err == nil
}

func GetVersion(ctx context.Context) (string, error) {
	cmd := newDockerCmdContext(ctx, "docker", "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func IsComposeInstalled(ctx context.Context) bool {
	cmd := newDockerCmdContext(ctx, "docker", "compose", "version")
	return cmd.Run() == nil
}

func Install() error {
	wizard.Info("Installing Docker Engine...")
	cmd := newDockerCmd("sh", "-c", "curl -fsSL https://get.docker.com | sh")
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Docker installation failed: %w", err)
	}
	wizard.Success("Docker installed successfully")
	return nil
}

func EnsureDocker(ctx context.Context) error {
	if !IsInstalled() {
		if !wizardConfirm("Docker not found. Install it?", true) {
			return fmt.Errorf("Docker is required to continue")
		}
		if err := Install(); err != nil {
			return err
		}
	} else {
		ver, _ := GetVersion(ctx)
		wizard.Success(fmt.Sprintf("%s detected", ver))
	}

	// Check the compose plugin on BOTH paths. Previously this only ran after a
	// fresh install, so a host that already had the Docker engine but not the
	// compose plugin passed `init` cleanly and then failed at the first
	// deploy with a raw "docker: 'compose' is not a docker command" — after
	// the proxy had already been configured.
	if !IsComposeInstalled(ctx) {
		wizard.Fail("Docker Compose plugin not found.")
		wizard.Info("Install it with your package manager, e.g.:")
		wizard.Info("  apt-get install docker-compose-plugin   (Debian/Ubuntu)")
		wizard.Info("  dnf install docker-compose-plugin       (Fedora/RHEL)")
		return fmt.Errorf("Docker Compose plugin is required")
	}

	return nil
}

func NetworkExists(ctx context.Context, name string) bool {
	cmd := newDockerCmdContext(ctx, "docker", "network", "inspect", name)
	return cmd.Run() == nil
}

func CreateNetwork(ctx context.Context, name string) error {
	if NetworkExists(ctx, name) {
		return nil
	}
	cmd := newDockerCmdContext(ctx, "docker", "network", "create", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create network %s: %s: %w", name, string(output), err)
	}
	wizard.Success(fmt.Sprintf("Network '%s' created", name))
	return nil
}
