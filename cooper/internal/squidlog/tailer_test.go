package squidlog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTailerStartsAtLast200ThenFollowsAndRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "access.log")
	var content strings.Builder
	for i := 0; i < 800; i++ {
		fmt.Fprintf(&content, "line-%03d\n", i)
	}
	if err := os.WriteFile(path, []byte(content.String()), 0600); err != nil {
		t.Fatal(err)
	}
	tailer := NewTailer(dir)
	tailer.poll = 5 * time.Millisecond
	tailer.Start()
	t.Cleanup(tailer.Stop)
	for i := 600; i < 800; i++ {
		expectLine(t, tailer, fmt.Sprintf("line-%03d", i))
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("next"); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-tailer.Lines():
		t.Fatalf("partial line emitted: %q", line)
	case <-time.After(30 * time.Millisecond):
	}
	if _, err := f.WriteString("-line\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	expectLine(t, tailer, "next-line")
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("rotated\n"), 0600); err != nil {
		t.Fatal(err)
	}
	expectLine(t, tailer, "rotated")
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if err := os.WriteFile(path, []byte("new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	expectLine(t, tailer, "new")
	tailer.Stop()
	select {
	case _, open := <-tailer.Lines():
		if open {
			t.Fatal("channel not closed")
		}
	case <-time.After(time.Second):
		t.Fatal("tailer did not stop")
	}
}

func TestTailerWaitsForNewFile(t *testing.T) {
	dir := t.TempDir()
	tailer := NewTailer(dir)
	tailer.poll = 5 * time.Millisecond
	tailer.Start()
	defer tailer.Stop()
	if err := os.WriteFile(filepath.Join(dir, "access.log"), []byte("created\n"), 0600); err != nil {
		t.Fatal(err)
	}
	expectLine(t, tailer, "created")
}

func TestSeekLastLines(t *testing.T) {
	for _, input := range []string{"", "one", "one\n", "one\ntwo\nthree", "one\ntwo\nthree\n", strings.Repeat("x", 70000) + "\ntwo\nthree\n"} {
		f, err := os.CreateTemp(t.TempDir(), "log")
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString(input)
		if err := seekLastLines(f, 2); err != nil {
			t.Fatal(err)
		}
		offset, _ := f.Seek(0, 1)
		lines := strings.Split(strings.TrimSuffix(input, "\n"), "\n")
		start := max(0, len(lines)-2)
		want := strings.Join(lines[start:], "\n")
		if strings.HasSuffix(input, "\n") {
			want += "\n"
		}
		if got := input[offset:]; got != want {
			t.Errorf("tail mismatch at offset %d", offset)
		}
		f.Close()
	}
}

func expectLine(t *testing.T, tailer *Tailer, want string) {
	t.Helper()
	select {
	case got := <-tailer.Lines():
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("no line %q", want)
	}
}
