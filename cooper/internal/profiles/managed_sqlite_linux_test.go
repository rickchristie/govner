package profiles

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedSQLiteWALSurvivesSwitchAndCanonicalOpen(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is needed for SQLite")
	}
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.save("codex")
	f.migrate()
	path := filepath.Join(f.home, ".codex", "session.db")
	writer := exec.Command(python, "-c", `import sqlite3,sys,os
c=sqlite3.connect(sys.argv[1]);c.execute('pragma journal_mode=wal');c.execute('pragma wal_autocheckpoint=0');c.execute('create table sessions (text)');c.execute("insert into sessions values ('committed before exit')");c.commit();os._exit(0)`, path)
	if output, err := writer.CombinedOutput(); err != nil {
		t.Fatalf("write SQLite: %v %s", err, output)
	}
	if info, err := os.Stat(path + "-wal"); err != nil || info.Size() == 0 {
		t.Fatal("no WAL fixture", err)
	}
	f.load("codex", "Work")
	f.load("codex", "Default")
	canonical := f.profilePath("Default", "codex-state", "session.db")
	for _, source := range []string{path, canonical} {
		reader := exec.Command(python, "-c", `import sqlite3,sys;c=sqlite3.connect(sys.argv[1]);print(c.execute('pragma integrity_check').fetchone()[0]);print(c.execute('select text from sessions').fetchone()[0])`, source)
		output, err := reader.CombinedOutput()
		if err != nil || strings.TrimSpace(string(output)) != "ok\ncommitted before exit" {
			t.Fatalf("read SQLite: %v %s", err, output)
		}
	}
}
