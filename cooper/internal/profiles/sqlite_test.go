package profiles

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompleteRootCopyIncludesCommittedSQLiteWAL(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is needed for the SQLite file fixture")
	}
	source, target := t.TempDir(), filepath.Join(t.TempDir(), "copy")
	script := `import sqlite3, sys
c = sqlite3.connect(sys.argv[1])
c.execute('pragma journal_mode=wal')
c.execute('pragma wal_autocheckpoint=0')
c.execute('create table sessions (message text)')
c.execute("insert into sessions values ('preserved session')")
c.commit()
print('ready', flush=True)
sys.stdin.read()
`
	writer := exec.Command(python, "-c", script, filepath.Join(source, "sessions.db"))
	input, err := writer.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := writer.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = input.Close(); _ = writer.Wait() }()
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || line != "ready\n" {
		t.Fatalf("SQLite fixture: %q %v", line, err)
	}
	if info, err := os.Stat(filepath.Join(source, "sessions.db-wal")); err != nil || info.Size() == 0 {
		t.Fatal("fixture has no WAL data")
	}
	if err := copyTree(t.Context(), source, target); err != nil {
		t.Fatal(err)
	}
	reader := exec.Command(python, "-c", `import sqlite3, sys; c=sqlite3.connect(sys.argv[1]); print(c.execute('pragma integrity_check').fetchone()[0]); print(c.execute('select message from sessions').fetchone()[0])`, filepath.Join(target, "sessions.db"))
	result, err := reader.CombinedOutput()
	if err != nil || strings.TrimSpace(string(result)) != "ok\npreserved session" {
		t.Fatalf("SQLite copy: %q %v", result, err)
	}
}

func TestRenamePreservesCommittedSQLiteWAL(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is needed for the SQLite fixture")
	}
	f := newFixture(t)
	f.write(".codex/account", "personal")
	path := filepath.Join(f.home, ".codex", "sessions.db")
	// Exit without closing SQLite so committed data remains in the WAL.
	script := `import sqlite3, sys, os
c = sqlite3.connect(sys.argv[1])
c.execute('pragma journal_mode=wal')
c.execute('pragma wal_autocheckpoint=0')
c.execute('create table sessions (message text)')
c.execute("insert into sessions values ('preserved session')")
c.commit()
os._exit(0)
`
	if output, err := exec.Command(python, "-c", script, path).CombinedOutput(); err != nil {
		t.Fatalf("SQLite fixture: %v %s", err, output)
	}
	id := inode(t, path+"-wal")
	f.save("codex")
	f.load("codex", "Work")
	f.load("codex", "Default")
	if inode(t, path+"-wal") != id {
		t.Fatal("switch copied or lost the WAL")
	}
	reader := exec.Command(python, "-c", `import sqlite3, sys; c=sqlite3.connect(sys.argv[1]); print(c.execute('pragma integrity_check').fetchone()[0]); print(c.execute('select message from sessions').fetchone()[0])`, path)
	if output, err := reader.CombinedOutput(); err != nil || strings.TrimSpace(string(output)) != "ok\npreserved session" {
		t.Fatalf("SQLite after switch: %v %s", err, output)
	}
}
