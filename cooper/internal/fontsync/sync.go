package fontsync

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Result holds the outcome of a font sync operation.
type Result struct {
	Copied   int
	Skipped  int
	Warnings []string
}

// fontExtensions are the file extensions considered as font files.
var fontExtensions = map[string]bool{
	".ttf": true,
	".otf": true,
	".ttc": true,
	".otc": true,
}

// Source describes a font source directory and the prefix name used
// in the destination to avoid filename collisions.
type Source struct {
	Path   string
	Prefix string
}

// LinuxSources returns the standard Linux font source directories.
func LinuxSources(homeDir string) []Source {
	return []Source{
		{filepath.Join(homeDir, ".local", "share", "fonts"), "user-local-share-fonts"},
		{filepath.Join(homeDir, ".fonts"), "user-dot-fonts"},
		{"/usr/local/share/fonts", "usr-local-share-fonts"},
		{"/usr/share/fonts", "usr-share-fonts"},
	}
}

// SyncLinuxFonts copies font files from standard Linux font directories into
// the Cooper-managed font directory at cooperDir/fonts. It preserves directory
// structure under source-specific prefixes to avoid filename collisions.
//
// Rules:
//   - sync is best-effort, not fatal for unreadable source roots
//   - copies only when destination is missing or source size/modtime differs
//   - never deletes files from the destination (preserves user-added fonts)
//   - returns warnings for unreadable source roots instead of errors
func SyncLinuxFonts(homeDir, cooperDir string) (Result, error) {
	return SyncFonts(LinuxSources(homeDir), cooperDir)
}

// SyncFonts copies font files from the given source directories into the
// Cooper-managed font directory at cooperDir/fonts.
func SyncFonts(sources []Source, cooperDir string) (Result, error) {
	dstRoot := filepath.Join(cooperDir, "fonts")
	if err := os.MkdirAll(dstRoot, 0o755); err != nil {
		return Result{}, fmt.Errorf("create font destination: %w", err)
	}

	var result Result
	for _, src := range sources {
		info, err := os.Stat(src.Path)
		if err != nil || !info.IsDir() {
			result.Warnings = append(result.Warnings, fmt.Sprintf("font source %s: not accessible", src.Path))
			continue
		}

		err = filepath.Walk(src.Path, func(path string, fi os.FileInfo, walkErr error) error {
			if walkErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("walk %s: %v", path, walkErr))
				return nil // continue walking
			}
			if fi.IsDir() {
				return nil
			}

			ext := strings.ToLower(filepath.Ext(fi.Name()))
			if !fontExtensions[ext] {
				return nil
			}

			// Compute relative path from source root.
			rel, err := filepath.Rel(src.Path, path)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("rel path %s: %v", path, err))
				return nil
			}

			dstPath := filepath.Join(dstRoot, src.Prefix, rel)

			// Check if copy is needed.
			if !needsCopy(fi, dstPath) {
				result.Skipped++
				return nil
			}

			// Create destination directory.
			if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("mkdir %s: %v", filepath.Dir(dstPath), err))
				return nil
			}

			// Copy the file.
			if err := copyFile(path, dstPath, fi); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("copy %s: %v", path, err))
				return nil
			}

			result.Copied++
			return nil
		})
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("walk %s: %v", src.Path, err))
		}
	}

	return result, nil
}

// needsCopy returns true if the source file should be copied to dstPath,
// either because the destination doesn't exist or the size/modtime differ.
func needsCopy(srcInfo os.FileInfo, dstPath string) bool {
	dstInfo, err := os.Stat(dstPath)
	if err != nil {
		return true // destination doesn't exist
	}
	if srcInfo.Size() != dstInfo.Size() {
		return true
	}
	if !srcInfo.ModTime().Equal(dstInfo.ModTime()) {
		return true
	}
	return false
}

// copyFile copies src to dst, preserving the modification time.
func copyFile(src, dst string, srcInfo os.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	// Preserve modification time for future skip detection.
	return os.Chtimes(dst, srcInfo.ModTime(), srcInfo.ModTime())
}
