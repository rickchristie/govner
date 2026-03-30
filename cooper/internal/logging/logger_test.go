package logging

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLogger_BasicWrite(t *testing.T) {
	dir := t.TempDir()
	l := NewLogger(dir, "test", 1<<20, 3) // 1MB maxSize, 3 maxFiles
	defer l.Close()

	l.Log("hello world")

	path := filepath.Join(dir, "test.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected log file to exist: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "hello world") {
		t.Fatalf("log file should contain 'hello world', got: %s", content)
	}

	// Verify RFC3339 timestamp prefix. The line format is "<RFC3339> <entry>\n".
	line := strings.TrimSpace(content)
	parts := strings.SplitN(line, " ", 2)
	if len(parts) < 2 {
		t.Fatalf("expected '<timestamp> <entry>', got: %s", line)
	}
	if _, err := time.Parse(time.RFC3339, parts[0]); err != nil {
		t.Fatalf("timestamp %q is not valid RFC3339: %v", parts[0], err)
	}
}

func TestLogger_MultipleWrites(t *testing.T) {
	dir := t.TempDir()
	l := NewLogger(dir, "test", 1<<20, 3)
	defer l.Close()

	for i := 0; i < 10; i++ {
		l.Log(fmt.Sprintf("entry-%d", i))
	}

	path := filepath.Join(dir, "test.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 10 {
		t.Fatalf("expected 10 lines, got %d", len(lines))
	}

	for i, line := range lines {
		// Each line should contain the entry text in order.
		expected := fmt.Sprintf("entry-%d", i)
		if !strings.Contains(line, expected) {
			t.Errorf("line %d: expected to contain %q, got %q", i, expected, line)
		}

		// Each line should have a timestamp prefix.
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			t.Errorf("line %d: expected '<timestamp> <entry>', got %q", i, line)
			continue
		}
		if _, err := time.Parse(time.RFC3339, parts[0]); err != nil {
			t.Errorf("line %d: timestamp %q is not valid RFC3339: %v", i, parts[0], err)
		}
	}
}

func TestLogger_RotationAtMaxSize(t *testing.T) {
	dir := t.TempDir()
	l := NewLogger(dir, "test", 100, 3) // 100 bytes maxSize
	defer l.Close()

	// Write entries until we exceed 100 bytes. Each line is roughly:
	// "2026-03-30T00:00:00Z entryNN\n" (~32+ bytes)
	// So about 3-4 entries should exceed 100 bytes.
	for i := 0; i < 4; i++ {
		l.Log(fmt.Sprintf("entry-%d", i))
	}

	// At this point, the file should have exceeded 100 bytes.
	// Write one more entry to trigger rotation.
	l.Log("after-rotation")

	currentPath := filepath.Join(dir, "test.log")
	rotatedPath := filepath.Join(dir, "test.1.log")

	// Current log file should exist with the latest entry.
	currentData, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatalf("expected current log file to exist: %v", err)
	}
	if !strings.Contains(string(currentData), "after-rotation") {
		t.Errorf("current log should contain 'after-rotation', got: %s", string(currentData))
	}

	// Rotated file should exist with older entries.
	rotatedData, err := os.ReadFile(rotatedPath)
	if err != nil {
		t.Fatalf("expected rotated file to exist: %v", err)
	}
	if !strings.Contains(string(rotatedData), "entry-0") {
		t.Errorf("rotated log should contain 'entry-0', got: %s", string(rotatedData))
	}
}

func TestLogger_MaxFilesCleanup(t *testing.T) {
	dir := t.TempDir()
	l := NewLogger(dir, "test", 50, 2) // 50 bytes maxSize, keep 2 rotated files
	defer l.Close()

	// Each line is ~35+ bytes, so each entry triggers rotation after the
	// first one fills the file. Write enough to cause at least 4 rotations.
	for i := 0; i < 20; i++ {
		l.Log(fmt.Sprintf("entry-%02d", i))
	}

	// Should exist: test.log, test.1.log, test.2.log
	for _, name := range []string{"test.log", "test.1.log", "test.2.log"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected %s to exist", name)
		}
	}

	// Should NOT exist: test.3.log (oldest cleaned up because maxFiles=2)
	path3 := filepath.Join(dir, "test.3.log")
	if _, err := os.Stat(path3); !os.IsNotExist(err) {
		t.Errorf("expected test.3.log to NOT exist, but it does (err=%v)", err)
	}
}

func TestLogger_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	l := NewLogger(dir, "test", 1<<20, 3) // large maxSize to avoid rotation
	defer l.Close()

	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				l.Log(fmt.Sprintf("goroutine-%d-entry-%d", id, i))
			}
		}(g)
	}
	wg.Wait()

	path := filepath.Join(dir, "test.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1000 {
		t.Fatalf("expected 1000 lines, got %d", len(lines))
	}

	// Verify no garbled lines: each line should parse as a valid timestamp + entry.
	for i, line := range lines {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			t.Errorf("line %d: expected '<timestamp> <entry>', got %q", i, line)
			continue
		}
		if _, err := time.Parse(time.RFC3339, parts[0]); err != nil {
			t.Errorf("line %d: garbled timestamp %q: %v", i, parts[0], err)
		}
		if !strings.HasPrefix(parts[1], "goroutine-") {
			t.Errorf("line %d: garbled entry %q", i, parts[1])
		}
	}
}

func TestLogger_Close(t *testing.T) {
	dir := t.TempDir()
	l := NewLogger(dir, "test", 1<<20, 3)

	l.Log("hello")

	if err := l.Close(); err != nil {
		t.Fatalf("first Close() should not error: %v", err)
	}

	// A second Close() should not error (file is already nil).
	if err := l.Close(); err != nil {
		t.Fatalf("second Close() should not error: %v", err)
	}
}

func TestLogger_LazyOpen(t *testing.T) {
	dir := t.TempDir()
	_ = NewLogger(dir, "test", 1<<20, 3)

	// Before any write, the log file should not exist.
	path := filepath.Join(dir, "test.log")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected log file to NOT exist before first write, err=%v", err)
	}

	// Now create a fresh logger and write.
	l := NewLogger(dir, "test", 1<<20, 3)
	defer l.Close()
	l.Log("first entry")

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("expected log file to exist after first write")
	}
}

func TestLogger_CreatesMissingDirectory(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "deeply", "nested", "logdir")

	l := NewLogger(dir, "test", 1<<20, 3)
	defer l.Close()

	l.Log("entry in new dir")

	path := filepath.Join(dir, "test.log")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected log file to exist in created directory: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("log file should not be empty")
	}
}

func TestLogger_RotationPreservesContent(t *testing.T) {
	dir := t.TempDir()
	// Each log line is ~32 bytes ("2026-03-30T00:00:00Z AAAA-entry\n").
	// With maxSize=60, two entries (~64 bytes) will exceed the limit.
	// The rotation check (size >= maxSize) fires before each write, so:
	//   - Write A: size goes from 0 to ~32 (no rotation, 0 < 60)
	//   - Write B: size goes from ~32 to ~64 (no rotation, 32 < 60)
	//   - Write C: size is ~64 >= 60, triggers rotation, then writes C to new file
	l := NewLogger(dir, "test", 60, 3)
	defer l.Close()

	l.Log("AAAA-entry")
	l.Log("BBBB-entry")

	// Read current file content before the rotation-triggering write.
	currentPath := filepath.Join(dir, "test.log")
	preRotation, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatalf("failed to read pre-rotation log: %v", err)
	}

	// This write should trigger rotation (size >= maxSize check happens before write).
	l.Log("CCCC-entry")

	// After rotation:
	// - test.1.log should contain A and B (pre-rotation content)
	// - test.log should contain C
	rotatedPath := filepath.Join(dir, "test.1.log")
	rotatedData, err := os.ReadFile(rotatedPath)
	if err != nil {
		t.Fatalf("expected rotated file to exist: %v", err)
	}

	if string(rotatedData) != string(preRotation) {
		t.Errorf("rotated file content should match pre-rotation content.\nExpected:\n%s\nGot:\n%s",
			string(preRotation), string(rotatedData))
	}

	if !strings.Contains(string(rotatedData), "AAAA-entry") {
		t.Errorf("rotated file should contain AAAA-entry")
	}
	if !strings.Contains(string(rotatedData), "BBBB-entry") {
		t.Errorf("rotated file should contain BBBB-entry")
	}

	currentData, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatalf("failed to read current log after rotation: %v", err)
	}
	if !strings.Contains(string(currentData), "CCCC-entry") {
		t.Errorf("current log should contain CCCC-entry, got: %s", string(currentData))
	}
	if strings.Contains(string(currentData), "AAAA-entry") || strings.Contains(string(currentData), "BBBB-entry") {
		t.Errorf("current log should NOT contain pre-rotation entries")
	}
}

func TestLogger_TimestampFormat(t *testing.T) {
	dir := t.TempDir()
	l := NewLogger(dir, "test", 1<<20, 3)
	defer l.Close()

	before := time.Now().UTC()
	l.Log("timestamp-check")
	after := time.Now().UTC()

	path := filepath.Join(dir, "test.log")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("failed to open log file: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		t.Fatalf("expected at least one line in log file")
	}
	line := scanner.Text()

	parts := strings.SplitN(line, " ", 2)
	if len(parts) < 2 {
		t.Fatalf("expected '<timestamp> <entry>', got: %s", line)
	}

	ts, err := time.Parse(time.RFC3339, parts[0])
	if err != nil {
		t.Fatalf("timestamp %q is not valid RFC3339: %v", parts[0], err)
	}

	// The timestamp should be between before and after (with some tolerance).
	if ts.Before(before.Add(-1*time.Second)) || ts.After(after.Add(1*time.Second)) {
		t.Errorf("timestamp %v is not within 1 second of now (before=%v, after=%v)", ts, before, after)
	}
}
