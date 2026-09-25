package chatgpt

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/rickchristie/govner/cooper/internal/testkeyring"
)

type testSecret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

type testKeyring struct {
	unlocked, locked []dbus.ObjectPath
	secret           testSecret
}

func (s testKeyring) SearchItems(attributes map[string]string) ([]dbus.ObjectPath, []dbus.ObjectPath, *dbus.Error) {
	want := map[string]string{"application": "chromium", "xdg:schema": "chrome_libsecret_os_crypt_password_v2"}
	if !reflect.DeepEqual(attributes, want) {
		return nil, nil, dbus.MakeFailedError(os.ErrPermission)
	}
	return s.unlocked, s.locked, nil
}

func (s testKeyring) OpenSession(algorithm string, input dbus.Variant) (dbus.Variant, dbus.ObjectPath, *dbus.Error) {
	if algorithm != "plain" || input.Value() != "" {
		return dbus.Variant{}, "", dbus.MakeFailedError(os.ErrPermission)
	}
	return dbus.MakeVariant(""), "/session", nil
}

func (s testKeyring) GetSecret(session dbus.ObjectPath) (testSecret, *dbus.Error) {
	if session != "/session" {
		return testSecret{}, dbus.MakeFailedError(os.ErrPermission)
	}
	return s.secret, nil
}

func (s testKeyring) Close() *dbus.Error { return nil }

func TestCookieKeyUsesPrivateSecretService(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	server, address := testkeyring.Start(t)
	client, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	for _, test := range []struct {
		name             string
		unlocked, locked []dbus.ObjectPath
		value            string
		wantError        bool
	}{
		{"exact key", []dbus.ObjectPath{"/item"}, nil, "synthetic-key", false},
		{"locked", nil, []dbus.ObjectPath{"/item"}, "synthetic-key", true},
		{"ambiguous", []dbus.ObjectPath{"/item", "/other"}, nil, "synthetic-key", true},
		{"missing", nil, nil, "", true},
		{"invalid secret", []dbus.ObjectPath{"/item"}, nil, "line\nbreak", true},
		{"empty secret", []dbus.ObjectPath{"/item"}, nil, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := testKeyring{test.unlocked, test.locked, testSecret{"/session", nil, []byte(test.value), "text/plain"}}
			for _, item := range []struct {
				path dbus.ObjectPath
				api  string
			}{
				{"/org/freedesktop/secrets", "Service"}, {"/item", "Item"}, {"/session", "Session"},
			} {
				if err := server.Export(service, item.path, "org.freedesktop.Secret."+item.api); err != nil {
					t.Fatal(err)
				}
			}
			key, err := readCookieKey(ctx, client)
			if (err != nil) != test.wantError || (!test.wantError && string(key) != test.value) {
				t.Fatalf("key read success = %v, expected success = %v", err == nil, !test.wantError)
			}
		})
	}
}

func TestCookieKeyFindsPersistentBrowserPartitions(t *testing.T) {
	for _, prefix := range []string{"Partitions", "Default/Partitions"} {
		t.Run(prefix, func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, prefix, "browser-app", "Network")
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "Cookies-wal"), []byte("synthetic v11 cookie"), 0600); err != nil {
				t.Fatal(err)
			}
			if needed, err := needsCookieKey(root); err != nil || !needed {
				t.Fatalf("needed = %v, error = %v", needed, err)
			}
		})
	}
}
