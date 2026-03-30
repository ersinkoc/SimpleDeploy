package docker

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

func BuildImage(contextDir, appName string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	tag := fmt.Sprintf("%s:%s", appName, timestamp)

	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
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

func BuildImageWithDockerfile(contextDir, dockerfilePath, appName string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	tag := fmt.Sprintf("%s:%s", appName, timestamp)

	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()

	cmd := newDockerCmdContext(ctx, "docker", "build", "-f", dockerfilePath, "-t", tag, contextDir)
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

func TagImage(source, target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), tagTimeout)
	defer cancel()
	cmd := newDockerCmdContext(ctx, "docker", "tag", source, target)
	return cmd.Run()
}

func RemoveImage(tag string) error {
	ctx, cancel := context.WithTimeout(context.Background(), tagTimeout)
	defer cancel()
	cmd := newDockerCmdContext(ctx, "docker", "rmi", "-f", tag)
	return cmd.Run()
}

func ListImages(appName string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
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

func CleanupOldImages(appName string, keep int) error {
	images, err := ListImages(appName)
	if err != nil {
		return err
	}
	if len(images) <= keep {
		return nil
	}
	for i := keep; i < len(images); i++ {
		if err := RemoveImage(images[i]); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to remove old image %s: %v\n", images[i], err)
		}
	}
	return nil
}

func PullImage(image string) error {
	ctx, cancel := context.WithTimeout(context.Background(), pullTimeout)
	defer cancel()
	cmd := newDockerCmdContext(ctx, "docker", "pull", image)
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	return cmd.Run()
}
