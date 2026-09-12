package vm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
)

const AssetSchema = "1"

// Locked assets are immutable inputs for the first Linux x86-64 VM schema.
var (
	UbuntuGuestAsset = Asset{
		Name:   "ubuntu-24.04-server-cloudimg-amd64-20260801.img",
		URL:    "https://cloud-images.ubuntu.com/releases/noble/release-20260801/ubuntu-24.04-server-cloudimg-amd64.img",
		SHA256: "0533b0655c32e68b31d792ecd6ccfca95abdbc536c4446874fe0513bd4140ffe",
		Size:   624239616,
	}
	DockerEngineAsset = Asset{
		Name:   "docker-29.7.2.tgz",
		URL:    "https://download.docker.com/linux/static/stable/x86_64/docker-29.7.2.tgz",
		SHA256: "803d433f226db4776e1768fd319fc6c6e4935a456acf84fcc0080818b854bc8f",
		Size:   85700518,
	}
)

// Asset is one exact downloaded VM input.
type Asset struct {
	Name   string
	URL    string
	SHA256 string
	Size   int64
}

// AssetStore downloads and verifies VM assets.
type AssetStore struct {
	Dir    string
	Client *http.Client
}

// AssetDir returns the persistent Cooper VM asset directory.
func AssetDir(cooperDir string) string {
	return filepath.Join(cooperDir, "vm", "assets", "schema-"+AssetSchema)
}

// PreparedGuestPath returns the immutable prepared guest base path.
func PreparedGuestPath(cooperDir string) string {
	return filepath.Join(AssetDir(cooperDir), "cooper-guest-base.qcow2")
}

// Ensure returns a verified local asset. It uses an exclusive file lock and
// an atomic rename so concurrent preparation cannot expose partial data.
func (s AssetStore) Ensure(ctx context.Context, asset Asset) (string, error) {
	if err := validateAsset(asset); err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return "", fmt.Errorf("create VM asset directory: %w", err)
	}
	lockPath := filepath.Join(s.Dir, "."+asset.Name+".lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return "", fmt.Errorf("open VM asset lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return "", fmt.Errorf("lock VM asset: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	finalPath := filepath.Join(s.Dir, asset.Name)
	valid, err := fileMatches(finalPath, asset)
	if err != nil {
		return "", err
	}
	if valid {
		return finalPath, nil
	}
	if err := os.Remove(finalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("remove invalid VM asset: %w", err)
	}

	temporary, err := os.CreateTemp(s.Dir, "."+asset.Name+".*.part")
	if err != nil {
		return "", fmt.Errorf("create VM asset temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		temporary.Close()
		return "", fmt.Errorf("create VM asset request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		temporary.Close()
		return "", fmt.Errorf("download VM asset %s: %w", asset.Name, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		temporary.Close()
		return "", fmt.Errorf("download VM asset %s: HTTP %s", asset.Name, response.Status)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(response.Body, asset.Size+1))
	if copyErr != nil {
		temporary.Close()
		return "", fmt.Errorf("write VM asset %s: %w", asset.Name, copyErr)
	}
	if closeErr := temporary.Close(); closeErr != nil {
		return "", fmt.Errorf("close VM asset %s: %w", asset.Name, closeErr)
	}
	if written != asset.Size {
		return "", fmt.Errorf("VM asset %s size is %d; want %d", asset.Name, written, asset.Size)
	}
	gotHash := hex.EncodeToString(hash.Sum(nil))
	if gotHash != asset.SHA256 {
		return "", fmt.Errorf("VM asset %s SHA-256 is %s; want %s", asset.Name, gotHash, asset.SHA256)
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return "", fmt.Errorf("set VM asset mode: %w", err)
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return "", fmt.Errorf("install VM asset: %w", err)
	}
	return finalPath, nil
}

func validateAsset(asset Asset) error {
	if asset.Name == "" || asset.Name == "." || asset.Name == ".." || filepath.Base(asset.Name) != asset.Name {
		return fmt.Errorf("invalid VM asset name %q", asset.Name)
	}
	if asset.URL == "" {
		return fmt.Errorf("VM asset %s has no URL", asset.Name)
	}
	if asset.Size <= 0 {
		return fmt.Errorf("VM asset %s has invalid size %d", asset.Name, asset.Size)
	}
	decoded, err := hex.DecodeString(asset.SHA256)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("VM asset %s has invalid SHA-256", asset.Name)
	}
	return nil
}

func fileMatches(path string, asset Asset) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("open VM asset %s: %w", path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, fmt.Errorf("stat VM asset %s: %w", path, err)
	}
	if info.Size() != asset.Size {
		return false, nil
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, fmt.Errorf("hash VM asset %s: %w", path, err)
	}
	return hex.EncodeToString(hash.Sum(nil)) == asset.SHA256, nil
}
