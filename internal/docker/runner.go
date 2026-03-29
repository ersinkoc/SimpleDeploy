package docker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const composeTimeout = 5 * time.Minute

func ComposeUp(composeDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), composeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "compose", "up", "-d")
	cmd.Dir = composeDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("docker compose up timed out after %v", composeTimeout)
		}
		return fmt.Errorf("docker compose up failed: %w", err)
	}
	return nil
}

func ComposeDown(composeDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), composeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "compose", "down")
	cmd.Dir = composeDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("docker compose down timed out after %v", composeTimeout)
		}
		return fmt.Errorf("docker compose down failed: %w", err)
	}
	return nil
}

func ComposeRemove(composeDir string, volumes bool) error {
	args := []string{"compose", "down"}
	if volumes {
		args = append(args, "-v")
	}

	ctx, cancel := context.WithTimeout(context.Background(), composeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = composeDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("docker compose down timed out after %v", composeTimeout)
		}
		return err
	}
	return nil
}

func ComposeLogs(composeDir, serviceName string, follow bool) error {
	args := []string{"compose", "logs"}
	if follow {
		args = append(args, "-f")
	}
	if serviceName != "" {
		args = append(args, serviceName)
	}

	// No timeout for follow mode — user controls when to stop
	if follow {
		cmd := exec.Command("docker", args...)
		cmd.Dir = composeDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = composeDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func ContainerStatus(containerName string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Status}}", containerName)
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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "restart", containerName)
	return cmd.Run()
}

func StopContainer(containerName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "stop", containerName)
	return cmd.Run()
}

func ExecContainer(containerName string, cmdArgs ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	args := append([]string{"exec", containerName}, cmdArgs...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func ListContainers(labelFilter string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	args := []string{"ps", "--format", "{{.Names}}"}
	if labelFilter != "" {
		args = append(args, "--filter", fmt.Sprintf("label=%s", labelFilter))
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}
