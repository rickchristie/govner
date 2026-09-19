package profiles

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/rickchristie/govner/cooper/internal/profilelink"
)

// CanRemoveUnusedStore permits full config cleanup after a build that has
// never saved a profile. Any data, history, unknown entry, or invalid metadata
// keeps the existing refusal. The caller must hold the exclusive state lock.
func CanRemoveUnusedStore(cooperDir string) bool {
	store, err := profilelink.Open(cooperDir)
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	if err != nil {
		return false
	}
	defer store.Close()
	if err := profilelink.Ready(store); err != nil {
		return false
	}
	managed, err := profilelink.Managed(store)
	if err != nil || !managed {
		return false
	}
	view, err := profilelink.Read(store)
	if err != nil || view.Previous != "" || len(view.Bindings) != 0 {
		return false
	}
	state, err := viewIndex(view)
	if err != nil || len(state.Profiles) != 0 || len(state.Hosts) != 0 {
		return false
	}
	if err := privatePath(store, "recovery", false); err != nil {
		return false
	}
	entries, err := fs.ReadDir(store.FS(), "recovery")
	if err != nil || len(entries) != 1 || !entries[0].IsDir() || !storedID.MatchString(entries[0].Name()) {
		return false
	}
	recovery := filepath.Join("recovery", entries[0].Name())
	if err := privatePath(store, recovery, false); err != nil {
		return false
	}
	var txn managedTransaction
	if err := readJSON(store, filepath.Join(recovery, "managed.json"), &txn); err != nil {
		return false
	}
	if txn.Schema != profilelink.Schema || txn.ID != entries[0].Name() || txn.Operation != "migrate" || txn.OldView != "" || txn.NewView != view.ID || len(txn.Changes) != 0 || txn.Legacy == nil {
		return false
	}
	if _, err := validateIndex(*txn.Legacy); err != nil || len(txn.Legacy.Profiles) != 0 || len(txn.Legacy.Hosts) != 0 {
		return false
	}
	viewDir := filepath.Join("views", view.ID)
	allowed := map[string]fs.FileMode{
		".": fs.ModeDir, "index.json": 0, "current": fs.ModeSymlink,
		"views": fs.ModeDir, viewDir: fs.ModeDir,
		filepath.Join(viewDir, "roots"): fs.ModeDir, filepath.Join(viewDir, "state.json"): 0,
		"recovery": fs.ModeDir, recovery: fs.ModeDir, filepath.Join(recovery, "managed.json"): 0,
	}
	return fs.WalkDir(store.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		kind, found := allowed[path]
		if !found || entry.Type() != kind {
			return errors.New("profile store contains data outside empty build setup")
		}
		return nil
	}) == nil
}
