package docker

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func BuildImage(contextDir, appName string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	tag := fmt.Sprintf("%s:%s", appName, timestamp)

	cmd := exec.Command("docker", "build", "-t", tag, contextDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker build failed: %w", err)
	}

	return tag, nil
}

func BuildImageWithDockerfile(contextDir, dockerfilePath, appName string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	tag := fmt.Sprintf("%s:%s", appName, timestamp)

	cmd := exec.Command("docker", "build", "-f", dockerfilePath, "-t", tag, contextDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker build failed: %w", err)
	}

	return tag, nil
}

func TagImage(source, target string) error {
	cmd := exec.Command("docker", "tag", source, target)
	return cmd.Run()
}

func RemoveImage(tag string) error {
	cmd := exec.Command("docker", "rmi", "-f", tag)
	return cmd.Run()
}

func ListImages(appName string) ([]string, error) {
	cmd := exec.Command("docker", "images", "--format", "{{.Repository}}:{{.Tag}}", appName)
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
		_ = RemoveImage(images[i])
	}
	return nil
}

func PullImage(image string) error {
	cmd := exec.Command("docker", "pull", image)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
