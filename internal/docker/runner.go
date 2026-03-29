package docker

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func ComposeUp(composeDir string) error {
	cmd := exec.Command("docker", "compose", "up", "-d")
	cmd.Dir = composeDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose up failed: %w", err)
	}
	return nil
}

func ComposeDown(composeDir string) error {
	cmd := exec.Command("docker", "compose", "down")
	cmd.Dir = composeDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose down failed: %w", err)
	}
	return nil
}

func ComposeRemove(composeDir string, volumes bool) error {
	args := []string{"compose", "down"}
	if volumes {
		args = append(args, "-v")
	}
	cmd := exec.Command("docker", args...)
	cmd.Dir = composeDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func ComposeLogs(composeDir, serviceName string, follow bool) error {
	args := []string{"compose", "logs"}
	if follow {
		args = append(args, "-f")
	}
	if serviceName != "" {
		args = append(args, serviceName)
	}
	cmd := exec.Command("docker", args...)
	cmd.Dir = composeDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func ContainerStatus(containerName string) (string, error) {
	cmd := exec.Command("docker", "inspect", "-f", "{{.State.Status}}", containerName)
	output, err := cmd.Output()
	if err != nil {
		return "not found", nil
	}
	return strings.TrimSpace(string(output)), nil
}

func ContainerExists(containerName string) bool {
	status, _ := ContainerStatus(containerName)
	return status != "not found"
}

func RestartContainer(containerName string) error {
	cmd := exec.Command("docker", "restart", containerName)
	return cmd.Run()
}

func StopContainer(containerName string) error {
	cmd := exec.Command("docker", "stop", containerName)
	return cmd.Run()
}

func ExecContainer(containerName string, cmdArgs ...string) error {
	args := append([]string{"exec", containerName}, cmdArgs...)
	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func ListContainers(labelFilter string) ([]string, error) {
	args := []string{"ps", "--format", "{{.Names}}"}
	if labelFilter != "" {
		args = append(args, "--filter", fmt.Sprintf("label=%s", labelFilter))
	}
	cmd := exec.Command("docker", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var containers []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			containers = append(containers, line)
		}
	}
	return containers, nil
}

func Run(args []string) error {
	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func RunOutput(args []string) (string, error) {
	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}
