package profiles

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The fixture excludes Docker and process discovery. This measures bounded
// profile storage work, not total CLI latency or physical SSD writes.
func BenchmarkProfileRoundTrip(b *testing.B) {
	for _, files := range []int{0, 5000} {
		b.Run(fmt.Sprintf("history-%d", files), func(b *testing.B) {
			f := newFixture(b)
			f.write(".codex/account", "personal")
			for number := range files {
				f.write(fmt.Sprintf(".codex/sessions/%06d", number), "history")
			}
			large, err := os.Create(filepath.Join(f.home, ".codex", "large-session"))
			if err != nil {
				b.Fatal(err)
			}
			if err := large.Truncate(2 << 30); err != nil {
				b.Fatal(err)
			}
			large.Close()
			f.save("codex")
			f.load("codex", "Work")
			f.write(".codex/account", "work")
			f.save("codex")
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				f.load("codex", "Default")
				f.load("codex", "Work")
			}
		})
	}
}
