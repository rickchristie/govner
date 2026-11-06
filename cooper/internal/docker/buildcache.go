package docker

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/aitool"
)

type imageBuildRecord struct {
	Inputs string `json:"inputs"`
	Image  string `json:"image"`
}

// Record the inputs and resulting image ID only after a successful build.
// An image label alone is insufficient: a derived image inherits that label.
// Keep records outside contexts so writing them does not invalidate the build.
func imageBuildRecordPath(name, contextDir string) (string, error) {
	contextPath, err := filepath.Abs(contextDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(contextPath), ".cooper-build-cache", fmt.Sprintf("%x.json", sha256.Sum256([]byte(name)))), nil
}

func readImageBuildRecord(name, contextDir string) imageBuildRecord {
	path, err := imageBuildRecordPath(name, contextDir)
	if err != nil {
		return imageBuildRecord{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return imageBuildRecord{}
	}
	var record imageBuildRecord
	if json.Unmarshal(data, &record) != nil {
		return imageBuildRecord{}
	}
	return record
}

// Failure to save an optimization must not make a successful image build fail.
// A missing or partial record makes the next call use Docker's build checks.
func saveImageBuildRecord(name, contextDir, inputs string, emit func(string)) {
	if inputs == "" {
		return
	}
	err := writeImageBuildRecord(name, contextDir, inputs)
	if err != nil {
		emit(fmt.Sprintf("Cannot save build record for %s: %v. The next build will use Docker.", name, err))
	}
}

func writeImageBuildRecord(name, contextDir, inputs string) error {
	image, err := inspectBuildValue(name, "{{.Id}}")
	if err != nil {
		return err
	}
	path, err := imageBuildRecordPath(name, contextDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(imageBuildRecord{Inputs: inputs, Image: image})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// Custom images keep Docker's own build rules, including remote ADD sources.
func prepareImageBuild(name, dockerfile, contextDir string, buildArgs map[string]string, noCache bool, emit func(string)) ([]string, string, bool, error) {
	args, err := buildDockerArgs(name, dockerfile, contextDir, buildArgs, noCache)
	if err != nil {
		return nil, "", false, err
	}
	if !managedBuildImage(name) {
		return args, "", false, nil
	}
	inputs, err := imageBuildInputs(dockerfile, contextDir, buildArgs)
	if err != nil {
		// Unsupported context entries must still reach Docker. They can be
		// excluded by .dockerignore, whose rules this check does not interpret.
		emit(fmt.Sprintf("Cannot check build inputs for %s: %v. Using Docker build.", name, err))
		return args, "", false, nil
	}
	if !noCache {
		record := readImageBuildRecord(name, contextDir)
		image, imageErr := inspectBuildValue(name, "{{.Id}}")
		if imageErr == nil && record.Inputs == inputs && record.Image == image {
			emit(fmt.Sprintf("Reusing %s image (build inputs unchanged).", name))
			return nil, "", true, nil
		}
	}
	return args, inputs, false, nil
}

func managedBuildImage(name string) bool {
	if name == GetImageBase() || name == GetImageDesktopBase() || name == GetImageProxy() {
		return true
	}
	prefix := imagePrefix + "cooper-cli-"
	return strings.HasPrefix(name, prefix) && aitool.IsBuiltin(strings.TrimPrefix(name, prefix))
}

func inspectBuildValue(name, format string) (string, error) {
	out, err := exec.Command("docker", "image", "inspect", "--format", format, name).Output()
	return strings.TrimSpace(string(out)), err
}

// Hash file contents and permissions, not times. Template generation rewrites
// these files on each build. Include all context files for a conservative check:
// an extra or ignored file can cause a build, but cannot hide a changed input.
func imageBuildInputs(dockerfile, contextDir string, buildArgs map[string]string) (string, error) {
	data, err := os.ReadFile(dockerfile)
	if err != nil {
		return "", err
	}
	parents, err := buildParentImages(string(data))
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	// Change this schema when the input rules change. JSON sorts map keys.
	if err := json.NewEncoder(hash).Encode(struct {
		Schema     int
		Dockerfile string
		Args       map[string]string
		Parents    map[string]string
		Platform   string
	}{1, string(data), buildArgs, parents, os.Getenv("DOCKER_DEFAULT_PLATFORM")}); err != nil {
		return "", err
	}
	err = filepath.WalkDir(contextDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported build context entry %s", path)
		}
		relative, err := filepath.Rel(contextDir, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%q %d\n", filepath.ToSlash(relative), info.Mode())
		if info.IsDir() {
			return nil
		}
		fmt.Fprintf(hash, "%d\n", info.Size())
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(hash, file)
		return err
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// Generated Dockerfiles use literal FROM names. Track local parent IDs because
// an unchanged agent Dockerfile must rebuild after its shared base changes.
// Registry parents can exist only in BuildKit's cache; their declared reference
// is still part of the Dockerfile hash. --clean always bypasses image reuse.
func buildParentImages(dockerfile string) (map[string]string, error) {
	parents := make(map[string]string)
	stages := make(map[string]bool)
	for _, line := range strings.Split(dockerfile, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.EqualFold(fields[0], "FROM") {
			continue
		}
		if len(fields) != 2 && (len(fields) != 4 || !strings.EqualFold(fields[2], "AS")) {
			return nil, fmt.Errorf("unsupported FROM instruction")
		}
		parent := fields[1]
		if strings.ContainsAny(parent, "$\\") || strings.HasPrefix(parent, "--") {
			return nil, fmt.Errorf("FROM requires a literal image name")
		}
		if !stages[parent] && parent != "scratch" {
			id, err := inspectBuildValue(parent, "{{.Id}}")
			if err != nil && managedBuildImage(parent) {
				return nil, fmt.Errorf("cannot inspect parent image %s: %w", parent, err)
			}
			parents[parent] = id
		}
		if len(fields) == 4 {
			stages[fields[3]] = true
		}
	}
	return parents, nil
}
