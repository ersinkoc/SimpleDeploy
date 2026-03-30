package docker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

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

func GetVersion() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := newDockerCmdContext(ctx, "docker", "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func IsComposeInstalled() bool {
	cmd := newDockerCmd("docker", "compose", "version")
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

func EnsureDocker() error {
	if IsInstalled() {
		ver, _ := GetVersion()
		wizard.Success(fmt.Sprintf("%s detected", ver))
		return nil
	}

	if !wizardConfirm("Docker not found. Install it?", true) {
		return fmt.Errorf("Docker is required to continue")
	}

	if err := Install(); err != nil {
		return err
	}

	if !IsComposeInstalled() {
		wizard.Info("Docker Compose not found. Please install the Docker Compose plugin.")
		return fmt.Errorf("Docker Compose plugin is required")
	}

	return nil
}

func NetworkExists(name string) bool {
	cmd := newDockerCmd("docker", "network", "inspect", name)
	return cmd.Run() == nil
}

func CreateNetwork(name string) error {
	if NetworkExists(name) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := newDockerCmdContext(ctx, "docker", "network", "create", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create network %s: %s: %w", name, string(output), err)
	}
	wizard.Success(fmt.Sprintf("Network '%s' created", name))
	return nil
}
