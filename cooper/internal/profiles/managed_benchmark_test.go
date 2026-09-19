//go:build linux

package profiles

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The large tree is added after conversion. Benchmark setup is outside the
// timer; this measures only a normal switch with fixed metadata and identity.
func BenchmarkManagedSwitch(b *testing.B) {
	for _, files := range []int{0, 5000} {
		b.Run(fmt.Sprintf("history-files-%d", files), func(b *testing.B) {
			f := newFixture(b)
			f.write(".codex/account", "personal")
			f.save("codex")
			f.migrate()
			f.load("codex", "Work")
			f.write(".codex/account", "work")
			f.save("codex")
			root := filepath.Join(f.home, ".codex")
			for i := 0; i < files; i++ {
				if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("history-%06d", i)), []byte("history"), 0600); err != nil {
					b.Fatal(err)
				}
			}
			file, err := os.Create(filepath.Join(root, "large-session"))
			if err != nil {
				b.Fatal(err)
			}
			if err := file.Truncate(2 << 30); err != nil {
				b.Fatal(err)
			}
			file.Close()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				name := "Default"
				if i%2 == 1 {
					name = "Work"
				}
				if _, err := f.service.Load(context.Background(), LoadRequest{Harness: "codex", Name: name}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
