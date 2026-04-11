package testdocker

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestStageSharedTestCAReusesStableCAAcrossBuildDirResets(t *testing.T) {
	sharedCA := filepath.Join(t.TempDir(), "shared-ca")

	buildDir1 := filepath.Join(t.TempDir(), "build-1")
	mkdirAllForSharedCATest(t, buildDir1)
	if err := stageSharedTestCA(sharedCA, buildDir1); err != nil {
		t.Fatalf("stage shared test CA into first build dir: %v", err)
	}

	baseCert1 := readSharedCATestFile(t, filepath.Join(buildDir1, "base", "cooper-ca.pem"))
	proxyCert1 := readSharedCATestFile(t, filepath.Join(buildDir1, "proxy", "cooper-ca.pem"))
	proxyKey1 := readSharedCATestFile(t, filepath.Join(buildDir1, "proxy", "cooper-ca-key.pem"))

	if !bytes.Equal(baseCert1, proxyCert1) {
		t.Fatalf("first staged base and proxy certificates differ")
	}

	buildDir2 := filepath.Join(t.TempDir(), "build-2")
	mkdirAllForSharedCATest(t, buildDir2)
	if err := stageSharedTestCA(sharedCA, buildDir2); err != nil {
		t.Fatalf("stage shared test CA into second build dir: %v", err)
	}

	baseCert2 := readSharedCATestFile(t, filepath.Join(buildDir2, "base", "cooper-ca.pem"))
	proxyCert2 := readSharedCATestFile(t, filepath.Join(buildDir2, "proxy", "cooper-ca.pem"))
	proxyKey2 := readSharedCATestFile(t, filepath.Join(buildDir2, "proxy", "cooper-ca-key.pem"))

	if !bytes.Equal(baseCert1, baseCert2) {
		t.Fatalf("shared CA certificate rotated across build dir resets")
	}
	if !bytes.Equal(proxyCert1, proxyCert2) {
		t.Fatalf("shared proxy CA certificate rotated across build dir resets")
	}
	if !bytes.Equal(proxyKey1, proxyKey2) {
		t.Fatalf("shared proxy CA key rotated across build dir resets")
	}
}

func mkdirAllForSharedCATest(t *testing.T, buildDir string) {
	t.Helper()
	for _, dir := range []string{
		filepath.Join(buildDir, "base"),
		filepath.Join(buildDir, "proxy"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
}

func readSharedCATestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
