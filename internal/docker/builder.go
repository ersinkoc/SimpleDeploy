package docker

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

func BuildImage(ctx context.Context, contextDir, appName string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	tag := fmt.Sprintf("%s:%s", appName, timestamp)

	ctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()

	cmd := newDockerCmdContext(ctx, "docker", "build", "-t", tag, contextDir)
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("docker build timed out after %v", buildTimeout)
		}
		return "", fmt.Errorf("docker build failed: %w", err)
	}

	return tag, nil
}

func RemoveImage(ctx context.Context, tag string) error {
	ctx, cancel := context.WithTimeout(ctx, tagTimeout)
	defer cancel()
	cmd := newDockerCmdContext(ctx, "docker", "rmi", "-f", tag)
	return cmd.Run()
}

func ListImages(ctx context.Context, appName string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	cmd := newDockerCmdContext(ctx, "docker", "images", "--format", "{{.Repository}}:{{.Tag}}", appName)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var images []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			images = append(images, line)
		}
	}
	return images, nil
}

func CleanupOldImages(ctx context.Context, appName string, keep int) error {
	images, err := ListImages(ctx, appName)
	if err != nil {
		return err
	}
	if len(images) <= keep {
		return nil
	}
	for i := keep; i < len(images); i++ {
		if err := RemoveImage(ctx, images[i]); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to remove old image %s: %v\n", images[i], err)
		}
	}
	return nil
}
