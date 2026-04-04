package logging

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdLogger_LogStart(t *testing.T) {
	dir := t.TempDir()
	cl := NewCmdLogger(dir, "configure")
	defer cl.Close()

	cl.LogStart()

	data := readLog(t, dir, "configure.log")
	if !strings.Contains(data, "command=configure status=started") {
		t.Errorf("expected start entry, got:\n%s", data)
	}
}

func TestCmdLogger_LogStepOK(t *testing.T) {
	dir := t.TempDir()
	cl := NewCmdLogger(dir, "up")
	defer cl.Close()

	cl.LogStep(0, "Create networks", nil)
	cl.LogStep(1, "Start proxy", nil)

	data := readLog(t, dir, "up.log")
	if !strings.Contains(data, `command=up step=0 name="Create networks" status=ok`) {
		t.Errorf("missing step 0 ok entry, got:\n%s", data)
	}
	if !strings.Contains(data, `command=up step=1 name="Start proxy" status=ok`) {
		t.Errorf("missing step 1 ok entry, got:\n%s", data)
	}
}

func TestCmdLogger_LogStepError(t *testing.T) {
	dir := t.TempDir()
	cl := NewCmdLogger(dir, "up")
	defer cl.Close()

	cl.LogStep(2, "Verify CA certificate", errors.New("CA certificate not found"))

	data := readLog(t, dir, "up.log")
	if !strings.Contains(data, `command=up step=2 name="Verify CA certificate" status=error`) {
		t.Errorf("missing step error entry, got:\n%s", data)
	}
	if !strings.Contains(data, "CA certificate not found") {
		t.Errorf("missing error message, got:\n%s", data)
	}
}

func TestCmdLogger_LogDoneSuccess(t *testing.T) {
	dir := t.TempDir()
	cl := NewCmdLogger(dir, "configure")
	defer cl.Close()

	cl.LogStart()
	cl.LogStep(0, "Initialize", nil)
	cl.LogDone(nil)

	data := readLog(t, dir, "configure.log")
	if !strings.Contains(data, "command=configure status=done") {
		t.Errorf("missing done entry, got:\n%s", data)
	}
}

func TestCmdLogger_LogDoneFailure(t *testing.T) {
	dir := t.TempDir()
	cl := NewCmdLogger(dir, "up")
	defer cl.Close()

	cl.LogStart()
	err := errors.New("startup failed")
	cl.LogStep(1, "Start proxy", err)
	cl.LogDone(err)

	data := readLog(t, dir, "up.log")
	if !strings.Contains(data, "command=up status=failed err=startup failed") {
		t.Errorf("missing failed entry, got:\n%s", data)
	}
}

func TestCmdLogger_FullSequence(t *testing.T) {
	dir := t.TempDir()
	cl := NewCmdLogger(dir, "up")
	defer cl.Close()

	cl.LogStart()
	cl.LogStep(0, "Create networks", nil)
	cl.LogStep(1, "Start proxy", nil)
	cl.LogStep(2, "Verify CA certificate", errors.New("not found"))

	data := readLog(t, dir, "up.log")
	lines := strings.Split(strings.TrimSpace(data), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 log lines, got %d:\n%s", len(lines), data)
	}

	// Verify ordering: started, step 0 ok, step 1 ok, step 2 error.
	if !strings.Contains(lines[0], "status=started") {
		t.Errorf("line 0: expected started, got: %s", lines[0])
	}
	if !strings.Contains(lines[1], "step=0") || !strings.Contains(lines[1], "status=ok") {
		t.Errorf("line 1: expected step 0 ok, got: %s", lines[1])
	}
	if !strings.Contains(lines[2], "step=1") || !strings.Contains(lines[2], "status=ok") {
		t.Errorf("line 2: expected step 1 ok, got: %s", lines[2])
	}
	if !strings.Contains(lines[3], "step=2") || !strings.Contains(lines[3], "status=error") {
		t.Errorf("line 3: expected step 2 error, got: %s", lines[3])
	}
}

func TestCmdLogger_Rotation(t *testing.T) {
	dir := t.TempDir()
	// Create with a tiny maxSize to trigger rotation.
	cl := &CmdLogger{
		l:       NewLogger(dir, "up", 100, 3),
		command: "up",
	}
	defer cl.Close()

	// Write enough entries to trigger rotation.
	for i := 0; i < 10; i++ {
		cl.LogStep(i, "step", nil)
	}

	// Rotated file should exist.
	rotated := filepath.Join(dir, "up.1.log")
	if _, err := os.Stat(rotated); os.IsNotExist(err) {
		t.Errorf("expected rotated file up.1.log to exist")
	}
}

func TestCmdLogger_CreatesMissingDir(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "nested", "logs")

	cl := NewCmdLogger(dir, "configure")
	defer cl.Close()

	cl.LogStart()

	path := filepath.Join(dir, "configure.log")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("expected log file to be created in nested dir")
	}

	data := readLog(t, dir, "configure.log")
	if !strings.Contains(data, "command=configure status=started") {
		t.Errorf("expected start entry in nested dir log, got:\n%s", data)
	}
}

// readLog reads the contents of a log file and fails the test if it doesn't exist.
func readLog(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}
