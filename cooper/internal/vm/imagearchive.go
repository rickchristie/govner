package vm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const imageArchiveMetadataSchema = 1

// ImageArchive stores one exact agent image as a Docker archive.
type ImageArchive struct {
	ImageID string
	Path    string
}

// imageArchiveMetadata lets Cooper reject an interrupted or truncated cache
// without reading a multi-gigabyte archive on each warm VM start. The cache is
// Cooper-owned host data. The guest still verifies the loaded Docker image ID.
type imageArchiveMetadata struct {
	Schema           int    `json:"schema"`
	ImageID          string `json:"image_id"`
	Size             int64  `json:"size"`
	SHA256           string `json:"sha256"`
	ModifiedUnixNano int64  `json:"modified_unix_nano"`
}

// EnsureImageArchive resolves and exports an exact local Docker image.
func EnsureImageArchive(ctx context.Context, cooperDir, imageRef string, runner CommandRunner, out io.Writer) (ImageArchive, error) {
	runner = runnerOrSystem(runner)
	data, err := runner.Output(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", imageRef)
	if err != nil {
		return ImageArchive{}, fmt.Errorf("inspect agent image %s: %w", imageRef, err)
	}
	imageID := strings.TrimSpace(string(data))
	if !validImageID(imageID) {
		return ImageArchive{}, fmt.Errorf("agent image %s has invalid ID %q", imageRef, imageID)
	}
	dir := ImageCacheDir(cooperDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ImageArchive{}, fmt.Errorf("create VM image cache: %w", err)
	}
	path := filepath.Join(dir, strings.TrimPrefix(imageID, "sha256:")+".tar")
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return ImageArchive{}, fmt.Errorf("open VM image archive lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return ImageArchive{}, fmt.Errorf("lock VM image archive: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	reusable, err := imageArchiveReusable(path, imageID)
	if err != nil {
		return ImageArchive{}, err
	}
	if reusable {
		return ImageArchive{ImageID: imageID, Path: path}, nil
	}
	for _, stale := range []string{path, imageArchiveMetadataPath(path)} {
		if err := os.Remove(stale); err != nil && !errors.Is(err, os.ErrNotExist) {
			return ImageArchive{}, fmt.Errorf("remove invalid VM image cache %s: %w", stale, err)
		}
	}

	temporary, err := os.CreateTemp(dir, ".agent-image-*.tar.part")
	if err != nil {
		return ImageArchive{}, fmt.Errorf("create VM image archive: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if out == nil {
		out = os.Stderr
	}
	exportStarted := time.Now()
	hash := sha256.New()
	// Use the immutable ID, not imageRef. A tag can move after the first
	// inspection and would poison the cache for the old ID.
	if err := runner.Run(ctx, nil, io.MultiWriter(temporary, hash), out, "docker", "save", imageID); err != nil {
		temporary.Close()
		return ImageArchive{}, fmt.Errorf("export agent image %s: %w", imageRef, err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return ImageArchive{}, fmt.Errorf("sync VM image archive: %w", err)
	}
	if err := temporary.Chmod(0o444); err != nil {
		temporary.Close()
		return ImageArchive{}, fmt.Errorf("set VM image archive mode: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return ImageArchive{}, fmt.Errorf("close VM image archive: %w", err)
	}
	info, err := os.Stat(temporaryPath)
	if err != nil {
		return ImageArchive{}, fmt.Errorf("inspect VM image archive: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return ImageArchive{}, errors.New("docker exported an empty VM image archive")
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return ImageArchive{}, fmt.Errorf("install VM image archive: %w", err)
	}
	metadata := imageArchiveMetadata{
		Schema: imageArchiveMetadataSchema, ImageID: imageID, Size: info.Size(),
		SHA256: hex.EncodeToString(hash.Sum(nil)), ModifiedUnixNano: info.ModTime().UnixNano(),
	}
	if err := writeJSON(imageArchiveMetadataPath(path), metadata, 0o444); err != nil {
		return ImageArchive{}, fmt.Errorf("write VM image archive metadata: %w", err)
	}
	fmt.Fprintf(out, "Cooper VM: exported agent image archive in %s (%d bytes)\n", time.Since(exportStarted).Round(time.Millisecond), info.Size())
	return ImageArchive{ImageID: imageID, Path: path}, nil
}

func imageArchiveReusable(path, imageID string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("inspect VM image archive: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return false, nil
	}
	data, err := os.ReadFile(imageArchiveMetadataPath(path))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read VM image archive metadata: %w", err)
	}
	var metadata imageArchiveMetadata
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return false, nil
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return false, nil
	}
	if metadata.Schema != imageArchiveMetadataSchema || metadata.ImageID != imageID ||
		metadata.Size != info.Size() || metadata.ModifiedUnixNano != info.ModTime().UnixNano() ||
		!validSHA256(metadata.SHA256) {
		return false, nil
	}
	return true, nil
}

func imageArchiveMetadataPath(path string) string {
	return path + ".json"
}

func validImageID(value string) bool {
	if !strings.HasPrefix(value, "sha256:") {
		return false
	}
	return validSHA256(strings.TrimPrefix(value, "sha256:"))
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}
