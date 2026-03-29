package docker

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

func IsInstalled() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}

func GetVersion() (string, error) {
	cmd := exec.Command("docker", "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func IsComposeInstalled() bool {
	cmd := exec.Command("docker", "compose", "version")
	return cmd.Run() == nil
}

func Install() error {
	wizard.Info("Installing Docker Engine...")
	cmd := exec.Command("sh", "-c", "curl -fsSL https://get.docker.com | sh")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
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

	if !wizard.Confirm("Docker not found. Install it?", true) {
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
	cmd := exec.Command("docker", "network", "inspect", name)
	return cmd.Run() == nil
}

func CreateNetwork(name string) error {
	if NetworkExists(name) {
		return nil
	}
	cmd := exec.Command("docker", "network", "create", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create network %s: %s: %w", name, string(output), err)
	}
	wizard.Success(fmt.Sprintf("Network '%s' created", name))
	return nil
}
