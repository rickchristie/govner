package vm

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func copyFileAtomic(source, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	return writeReaderAtomic(input, target, mode)
}

func writeFileAtomic(data []byte, target string, mode os.FileMode) error {
	return writeReaderAtomic(bytes.NewReader(data), target, mode)
}

func writeReaderAtomic(input io.Reader, target string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".cooper-copy-*.part")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := io.Copy(temporary, input); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return fmt.Errorf("replace %s: %w", target, err)
	}
	return nil
}
