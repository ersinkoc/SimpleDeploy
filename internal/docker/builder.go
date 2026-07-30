package docker

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

func BuildImage(ctx context.Context, contextDir, appName string) (string, error) {
	// UTC, not local time. ListImages orders an app's images by reverse string
	// sort and CleanupOldImages trusts that as recency ordering, which only
	// holds if the timestamps are monotonic. A local-time stamp repeats the
	// same hour during a DST fall-back, so images built in that window sort as
	// older than they are and the prune keeps the wrong three.
	// Millisecond precision, not second: two builds of the same app in the same
	// wall-clock second (a CLI redeploy racing a webhook-triggered one, which
	// nothing serializes across processes) produced the SAME tag. Both builds
	// "succeeded", the tag ended up pointing at whichever finished last, the
	// other image was silently untagged, and both runs recorded that identical
	// tag as CurrentImage — so which code was actually deployed was
	// nondeterministic. The format stays lexically sortable, which ListImages
	// and CleanupOldImages depend on for recency ordering.
	timestamp := time.Now().UTC().Format("20060102-150405.000")
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
		if line != "" && line != "<none>:<none>" {
			images = append(images, line)
		}
	}
	// Newest first. BuildImage tags every image "<app>:<YYYYMMDD-HHMMSS.mmm>", a
	// format whose lexical order matches its chronological order, so a reverse
	// string sort is a reliable recency ordering. (Mixed old second-precision
	// and new millisecond tags still order correctly: for an equal
	// YYYYMMDD-HHMMSS prefix the longer millisecond string sorts greater, i.e.
	// newer, which is chronologically right.) CleanupOldImages depends on
	// this: it used to trust `docker images` output order, which is only
	// coincidentally recency-sorted and is not part of the CLI's contract —
	// meaning a prune could delete the image the app is currently running.
	sort.Sort(sort.Reverse(sort.StringSlice(images)))
	return images, nil
}

// CleanupOldImages removes all but the `keep` most recent images built for an
// app. Failure to remove an individual image is reported but not fatal:
// `docker rmi` refuses images that are still in use by a container, which is
// exactly what we want for the running deployment.
func CleanupOldImages(ctx context.Context, appName string, keep int) error {
	images, err := ListImages(ctx, appName)
	if err != nil {
		return err
	}
	if keep < 0 {
		keep = 0
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
