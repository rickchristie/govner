package docker

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

var (
	// imagePrefix is prepended to all Docker image/container names.
	// Set via SetImagePrefix() for testing with isolated names.
	imagePrefix = ""

	// Default image names (with prefix applied via functions).
	defaultImageProxy     = "cooper-proxy"
	defaultImageBarrelBase = "cooper-barrel-base"
	defaultImageBarrel     = "cooper-barrel"
)

// SetImagePrefix sets a prefix for all Docker image names.
// Used for testing to avoid colliding with real Cooper images.
// Example: SetImagePrefix("test-mirror-") → images become "test-mirror-cooper-proxy", etc.
// Note: container names are NOT prefixed (they're used for Docker DNS).
func SetImagePrefix(prefix string) {
	imagePrefix = prefix
}

// ImagePrefix returns the current image prefix.
func ImagePrefix() string {
	return imagePrefix
}

// ImageProxy returns the proxy image name (with prefix).
func GetImageProxy() string { return imagePrefix + defaultImageProxy }

// ImageBarrelBase returns the barrel base image name (with prefix).
func GetImageBarrelBase() string { return imagePrefix + defaultImageBarrelBase }

// ImageBarrel returns the barrel image name (with prefix).
func GetImageBarrel() string { return imagePrefix + defaultImageBarrel }

// Keep constants for backward compatibility — code that uses these directly
// will get the unprefixed names. New code should use Get*() functions.
const (
	ImageProxy     = "cooper-proxy"
	ImageBarrelBase = "cooper-barrel-base"
	ImageBarrel     = "cooper-barrel"
)

// BuildImage builds a Docker image with the given name from the specified
// Dockerfile and context directory. Build arguments are passed as --build-arg
// flags. Output is streamed to stderr for visibility during builds.
func BuildImage(name, dockerfilePath, contextDir string, buildArgs map[string]string, noCache bool) error {
	args := buildDockerArgs(name, dockerfilePath, contextDir, buildArgs, noCache)

	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build %s failed: %w", name, err)
	}
	return nil
}

// BuildImageWithOutput builds a Docker image and streams output line by line
// on a channel for TUI progress display. The string channel receives each
// line of combined stdout/stderr. The error channel receives nil on success
// or the build error. Both channels are closed when the build completes.
func BuildImageWithOutput(name, dockerfilePath, contextDir string, buildArgs map[string]string, noCache bool) (<-chan string, <-chan error) {
	lines := make(chan string, 64)
	errc := make(chan error, 1)

	go func() {
		defer close(errc)

		args := buildDockerArgs(name, dockerfilePath, contextDir, buildArgs, noCache)
		cmd := exec.Command("docker", args...)

		// Combine stdout and stderr into a single pipe for streaming.
		pr, pw := io.Pipe()
		cmd.Stdout = pw
		cmd.Stderr = pw

		if err := cmd.Start(); err != nil {
			errc <- fmt.Errorf("docker build %s failed to start: %w", name, err)
			close(lines)
			return
		}

		// Read output line by line and send on channel. The scanner
		// goroutine owns closing the lines channel to avoid a send-on-
		// closed-channel panic from the outer goroutine.
		scanner := bufio.NewScanner(pr)
		go func() {
			defer close(lines)
			for scanner.Scan() {
				lines <- scanner.Text()
			}
		}()

		// Wait for the command to finish and close the write end of the pipe
		// so the scanner goroutine exits.
		err := cmd.Wait()
		pw.Close()

		if err != nil {
			errc <- fmt.Errorf("docker build %s failed: %w", name, err)
			return
		}
		errc <- nil
	}()

	return lines, errc
}

// ImageExists checks whether a Docker image with the given name exists locally.
func ImageExists(name string) (bool, error) {
	cmd := exec.Command("docker", "image", "inspect", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// "No such image" means it doesn't exist -- not an error.
		if strings.Contains(string(output), "No such image") ||
			strings.Contains(string(output), "not found") {
			return false, nil
		}
		return false, fmt.Errorf("docker image inspect %s failed: %w\n%s", name, err, string(output))
	}
	return true, nil
}

// RemoveImage forcibly removes a Docker image.
func RemoveImage(name string) error {
	cmd := exec.Command("docker", "rmi", "-f", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rmi %s failed: %w\n%s", name, err, string(output))
	}
	return nil
}

// TagImage tags a source image with a new target name.
func TagImage(source, target string) error {
	cmd := exec.Command("docker", "tag", source, target)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker tag %s %s failed: %w\n%s", source, target, err, string(output))
	}
	return nil
}

// buildDockerArgs constructs the docker build argument list.
func buildDockerArgs(name, dockerfilePath, contextDir string, buildArgs map[string]string, noCache bool) []string {
	args := []string{"build", "-t", name, "-f", dockerfilePath}
	if noCache {
		args = append(args, "--no-cache")
	}
	for k, v := range buildArgs {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, contextDir)
	return args
}
