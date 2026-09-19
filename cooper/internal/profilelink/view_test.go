package profilelink

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func linkedFixture(t *testing.T) (string, string, View) {
	t.Helper()
	base := t.TempDir()
	cooper := filepath.Join(base, "cooper")
	host := filepath.Join(base, "state")
	binding := Binding{ID: strings.Repeat("a", 24), Path: host, Source: "harnesses/codex/" + strings.Repeat("b", 24) + "/" + strings.Repeat("c", 24) + "/roots/codex-state", ProfileID: strings.Repeat("b", 24), RootID: "codex-state", Kind: "directory"}
	view := View{Schema: Schema, ID: strings.Repeat("d", 24), Bindings: []Binding{binding}, Catalog: json.RawMessage(`{}`)}
	root := filepath.Join(cooper, "profiles")
	for _, path := range []string{filepath.Join(root, binding.Source), filepath.Join(root, "views", view.ID, "roots")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{"index.json": []byte(`{"schema":2}`), filepath.Join("views", view.ID, "state.json"): data} {
		if err := os.WriteFile(filepath.Join(root, path), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for path, target := range map[string]string{filepath.Join(root, "current"): "views/" + view.ID, filepath.Join(root, "views", view.ID, "roots", binding.ID): "../../../" + binding.Source, host: Alias(cooper, binding)} {
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
	}
	return cooper, host, view
}

func TestOnlyRegisteredCompleteRootIsResolved(t *testing.T) {
	cooper, host, view := linkedFixture(t)
	binding, managed, err := Resolve(host, cooper)
	if err != nil || !managed || binding.Source != view.Bindings[0].Source {
		t.Fatal(binding, managed, err)
	}
	if _, _, err := Resolve(filepath.Join(host, "child"), cooper); err == nil {
		t.Fatal("child accepted")
	}
	if _, _, err := Resolve(host, filepath.Join(t.TempDir(), "other")); err == nil {
		t.Fatal("other store accepted")
	}
	alias := filepath.Join(filepath.Dir(host), "user-alias")
	if err := os.Symlink(host, alias); err != nil {
		t.Fatal(err)
	}
	if _, managed, err := Resolve(alias, cooper); err != nil || !managed {
		t.Fatal("user alias rejected", err)
	}
}

func TestBrokenControlStateCannotFallBackToHost(t *testing.T) {
	for _, kind := range []string{"missing-index", "old-index", "bad-selector", "bad-link", "public-view", "root-symlink", "unknown-field", "journal"} {
		t.Run(kind, func(t *testing.T) {
			cooper, host, view := linkedFixture(t)
			store := filepath.Join(cooper, "profiles")
			binding := view.Bindings[0]
			switch kind {
			case "missing-index":
				os.Remove(filepath.Join(store, "index.json"))
			case "old-index":
				os.WriteFile(filepath.Join(store, "index.json"), []byte(`{"schema":1}`), 0600)
			case "bad-selector":
				os.Remove(filepath.Join(store, "current"))
				os.Symlink("../outside", filepath.Join(store, "current"))
			case "bad-link":
				path := filepath.Join(store, "views", view.ID, "roots", binding.ID)
				os.Remove(path)
				os.Symlink("../../../../credentials", path)
			case "public-view":
				os.Chmod(filepath.Join(store, "views", view.ID), 0755)
			case "root-symlink":
				path := filepath.Join(store, binding.Source)
				os.Remove(path)
				os.Symlink(t.TempDir(), path)
			case "unknown-field":
				data, _ := json.Marshal(view)
				data = append(data[:len(data)-1], []byte(`,"unknown":true}`)...)
				os.WriteFile(filepath.Join(store, "views", view.ID, "state.json"), data, 0600)
			case "journal":
				os.WriteFile(filepath.Join(store, "transaction.json"), []byte(`{}`), 0600)
			}
			if _, _, err := Resolve(host, cooper); err == nil {
				t.Fatal("bad managed state accepted")
			}
		})
	}
}

func TestMissingDescriptorAndSelectorCannotCreateEmptyStore(t *testing.T) {
	cooper, _, _ := linkedFixture(t)
	store, err := Open(cooper)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, name := range []string{"index.json", "current"} {
		if err := store.Remove(name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Managed(store); err == nil {
		t.Fatal("managed data without its descriptor was treated as an empty copy store")
	}
}
