package vme2e

import (
	"errors"
	"reflect"

	"github.com/godbus/dbus/v5"
	"github.com/rickchristie/govner/cooper/internal/testkeyring"
)

const desktopTestCookieKey = "cooper-synthetic-native-cookie-key"

type desktopSecretService struct{}
type desktopSecret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

func (desktopSecretService) SearchItems(attributes map[string]string) ([]dbus.ObjectPath, []dbus.ObjectPath, *dbus.Error) {
	if !reflect.DeepEqual(attributes, map[string]string{"application": "chromium", "xdg:schema": "chrome_libsecret_os_crypt_password_v2"}) {
		return nil, nil, dbus.MakeFailedError(errors.New("unexpected key lookup"))
	}
	return []dbus.ObjectPath{"/item"}, nil, nil
}
func (desktopSecretService) OpenSession(string, dbus.Variant) (dbus.Variant, dbus.ObjectPath, *dbus.Error) {
	return dbus.MakeVariant(""), "/session", nil
}
func (desktopSecretService) GetSecret(session dbus.ObjectPath) (desktopSecret, *dbus.Error) {
	return desktopSecret{session, nil, []byte(desktopTestCookieKey), "text/plain"}, nil
}
func (desktopSecretService) Close() *dbus.Error { return nil }

func (f *developmentFixture) desktopKeyring() {
	server, address := testkeyring.Start(f.t)
	f.t.Setenv("DBUS_SESSION_BUS_ADDRESS", address)
	for _, item := range []struct {
		path dbus.ObjectPath
		api  string
	}{
		{"/org/freedesktop/secrets", "Service"}, {"/item", "Item"}, {"/session", "Session"},
	} {
		if err := server.Export(desktopSecretService{}, item.path, "org.freedesktop.Secret."+item.api); err != nil {
			f.t.Fatal(err)
		}
	}
}
