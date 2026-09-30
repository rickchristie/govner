package buildlog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogRetainsFailureAndPreviousBuild(t *testing.T) {
	dir := t.TempDir()
	first, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("package output ", 10000)
	first.Line(line)
	if _, err := Open(dir); err == nil {
		t.Fatal("second build replaced an active log")
	}
	cause := errors.New("APT exited with code 100")
	if err := first.Finish(cause); !errors.Is(err, cause) || !strings.Contains(err.Error(), first.Path) {
		t.Fatalf("missing build error and path: %v", err)
	}
	data, err := os.ReadFile(first.Path)
	if err != nil || !strings.Contains(string(data), line) || !strings.Contains(string(data), cause.Error()) {
		t.Fatalf("incomplete log: %v", err)
	}
	info, err := os.Stat(first.Path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("build log is not private")
	}
	second, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	second.Line("next build")
	if err := second.Finish(nil); err != nil {
		t.Fatal(err)
	}
	previous, err := os.ReadFile(filepath.Join(dir, "logs", "build.previous.log"))
	if err != nil || string(previous) != string(data) {
		t.Fatal("previous build was not retained")
	}
	current, err := os.ReadFile(second.Path)
	if err != nil || strings.Contains(string(current), line) || !strings.Contains(string(current), "next build") {
		t.Fatal("current log contains the wrong build")
	}
	third, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := third.Finish(nil); err != nil {
		t.Fatal(err)
	}
	previous, err = os.ReadFile(filepath.Join(dir, "logs", "build.previous.log"))
	if err != nil || string(previous) != string(current) {
		t.Fatal("third build did not replace the oldest log")
	}
	paths, err := filepath.Glob(filepath.Join(dir, "logs", "*.log"))
	if err != nil || len(paths) != 2 {
		t.Fatalf("build log count = %d, want 2: %v", len(paths), err)
	}
}
