package docker

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func ComposeUp(ctx context.Context, composeDir string) error {
	ctx, cancel := context.WithTimeout(ctx, composeTimeout)
	defer cancel()

	cmd := newDockerCmdContext(ctx, "docker", "compose", "up", "-d")
	cmd.SetDir(composeDir)
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("docker compose up timed out after %v", composeTimeout)
		}
		return fmt.Errorf("docker compose up failed: %w", err)
	}
	return nil
}

func ComposeDown(ctx context.Context, composeDir string) error {
	ctx, cancel := context.WithTimeout(ctx, composeTimeout)
	defer cancel()

	cmd := newDockerCmdContext(ctx, "docker", "compose", "down")
	cmd.SetDir(composeDir)
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("docker compose down timed out after %v", composeTimeout)
		}
		return fmt.Errorf("docker compose down failed: %w", err)
	}
	return nil
}

func ComposeRemove(ctx context.Context, composeDir string, volumes bool) error {
	args := []string{"compose", "down"}
	if volumes {
		args = append(args, "-v")
	}

	ctx, cancel := context.WithTimeout(ctx, composeTimeout)
	defer cancel()

	cmd := newDockerCmdContext(ctx, "docker", args...)
	cmd.SetDir(composeDir)
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("docker compose down timed out after %v", composeTimeout)
		}
		return err
	}
	return nil
}

// logTailLines bounds a non-following log dump. Without it `docker compose
// logs` prints the container's entire history, which for a long-running app
// can be hundreds of megabytes scrolling past the operator.
const logTailLines = "200"

func ComposeLogs(ctx context.Context, composeDir, serviceName string, follow bool) error {
	args := []string{"compose", "logs"}
	if follow {
		args = append(args, "-f", "--tail", logTailLines)
	} else {
		args = append(args, "--tail", logTailLines)
	}
	if serviceName != "" {
		args = append(args, serviceName)
	}

	// No timeout for follow mode — user controls when to stop
	if follow {
		cmd := newDockerCmd("docker", args...)
		cmd.SetDir(composeDir)
		cmd.SetStdout(os.Stdout)
		cmd.SetStderr(os.Stderr)
		return cmd.Run()
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := newDockerCmdContext(ctx, "docker", args...)
	cmd.SetDir(composeDir)
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	return cmd.Run()
}

func ContainerStatus(ctx context.Context, containerName string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()

	cmd := newDockerCmdContext(ctx, "docker", "inspect", "-f", "{{.State.Status}}", containerName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if container doesn't exist vs other errors
		// Docker returns various error messages for non-existent containers:
		// - "no such container"
		// - "no such object"
		// - "not found"
		outStr := strings.ToLower(string(output))
		if strings.Contains(outStr, "no such") ||
			strings.Contains(outStr, "not found") {
			return "not found", nil
		}
		// Return actual error for other cases
		return "", fmt.Errorf("failed to get container status: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// ContainerRestartCount reports how many times Docker has restarted the
// container since it was created.
//
// This is the signal that distinguishes a crash-looping container from a
// healthy one. Every app service is generated with `restart: unless-stopped`,
// so a container whose process dies is put straight back into "restarting" and
// then briefly "running" — it never settles in "exited" or "dead". Status alone
// therefore cannot tell the two apart, and the restart counter is what survives
// between those flickers.
func ContainerRestartCount(ctx context.Context, containerName string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()

	cmd := newDockerCmdContext(ctx, "docker", "inspect", "-f", "{{.RestartCount}}", containerName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("failed to get restart count for %s: %w", containerName, err)
	}
	raw := strings.TrimSpace(string(output))
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("unexpected restart count %q for %s: %w", raw, containerName, err)
	}
	return n, nil
}

func ContainerExists(ctx context.Context, containerName string) bool {
	status, _ := ContainerStatus(ctx, containerName)
	return status != "not found"
}

func RestartContainer(ctx context.Context, containerName string) error {
	ctx, cancel := context.WithTimeout(ctx, containerTimeout)
	defer cancel()
	cmd := newDockerCmdContext(ctx, "docker", "restart", containerName)
	return cmd.Run()
}

func StopContainer(ctx context.Context, containerName string) error {
	ctx, cancel := context.WithTimeout(ctx, containerTimeout)
	defer cancel()
	cmd := newDockerCmdContext(ctx, "docker", "stop", containerName)
	return cmd.Run()
}

func ExecContainer(ctx context.Context, containerName string, cmdArgs ...string) error {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	args := append([]string{"exec", containerName}, cmdArgs...)
	cmd := newDockerCmdContext(ctx, "docker", args...)
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	return cmd.Run()
}

func ListContainers(ctx context.Context, labelFilter string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()

	args := []string{"ps", "--format", "{{.Names}}"}
	if labelFilter != "" {
		args = append(args, "--filter", fmt.Sprintf("label=%s", labelFilter))
	}
	cmd := newDockerCmdContext(ctx, "docker", args...)
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
	cmd := newDockerCmd("docker", args...)
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	return cmd.Run()
}

func RunOutput(ctx context.Context, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, runOutputTimeout)
	defer cancel()

	cmd := newDockerCmdContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}
