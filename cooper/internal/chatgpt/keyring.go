package chatgpt

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
)

const CookieKeyEnvironment = "COOPER_CHATGPT_COOKIE_KEY"

// CookieKey returns only the native Chromium encryption key, and only when
// the selected app has v11 cookies. Other keyring records are never listed.
// The value travels through the session exec channel, never Docker labels,
// image layers, saved Cooper config, or command output.
func CookieKey(ctx context.Context, directory string) (string, error) {
	needed, err := needsCookieKey(directory)
	if err != nil || !needed {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	connection, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return "", errors.New("ChatGPT has encrypted cookies; start and unlock the host Linux login keyring before launch")
	}
	defer connection.Close()
	key, err := readCookieKey(ctx, connection)
	if err != nil {
		return "", fmt.Errorf("read ChatGPT cookie key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

func needsCookieKey(directory string) (bool, error) {
	root, err := filepath.EvalSymlinks(directory)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	paths, err := cookiePaths(root)
	if err != nil {
		return false, err
	}
	remaining := int64(64 << 20)
	for _, relative := range paths {
		path := filepath.Join(root, relative)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if !info.Mode().IsRegular() {
			return false, errors.New("ChatGPT cookie storage must be a regular file")
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || resolved != path {
			return false, errors.New("ChatGPT cookie storage must stay inside the selected app root without child links")
		}
		file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return false, err
		}
		data, readErr := io.ReadAll(io.LimitReader(file, remaining+1))
		file.Close()
		if readErr != nil {
			return false, readErr
		}
		remaining -= int64(len(data))
		if remaining < 0 {
			return false, errors.New("ChatGPT cookie storage exceeds the bounded credential check")
		}
		if bytes.Contains(data, []byte("v11")) {
			return true, nil
		}
	}
	return false, nil
}

// Only cookie databases and their write-ahead logs need the encryption key.
// Persistent browser partitions have their own databases. Bound metadata and
// bytes without reading conversation history or unrelated browser storage.
func cookiePaths(root string) ([]string, error) {
	profiles := []string{"Default"}
	for _, relative := range []string{"Partitions", "Default/Partitions"} {
		path := filepath.Join(root, relative)
		resolved, err := filepath.EvalSymlinks(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || resolved != path {
			return nil, errors.New("ChatGPT cookie partitions must not use child links")
		}
		directory, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		entries, readErr := directory.ReadDir(257)
		directory.Close()
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, readErr
		}
		if len(profiles)+len(entries) > 257 {
			return nil, errors.New("ChatGPT cookie partitions exceed the bounded credential check")
		}
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 {
				return nil, errors.New("ChatGPT cookie partitions must not use child links")
			}
			if entry.IsDir() {
				profiles = append(profiles, filepath.Join(relative, entry.Name()))
			}
		}
	}
	var paths []string
	for _, profile := range profiles {
		for _, file := range []string{"Cookies", "Cookies-wal", "Network/Cookies", "Network/Cookies-wal"} {
			paths = append(paths, filepath.Join(profile, file))
		}
	}
	return paths, nil
}

func readCookieKey(ctx context.Context, connection *dbus.Conn) ([]byte, error) {
	service := connection.Object("org.freedesktop.secrets", "/org/freedesktop/secrets")
	attributes := map[string]string{"application": "chromium", "xdg:schema": "chrome_libsecret_os_crypt_password_v2"}
	var unlocked, locked []dbus.ObjectPath
	if err := service.CallWithContext(ctx, "org.freedesktop.Secret.Service.SearchItems", dbus.FlagNoAutoStart, attributes).Store(&unlocked, &locked); err != nil {
		return nil, errors.New("the host Secret Service is unavailable; unlock the Linux login keyring")
	}
	if len(locked) > 0 {
		return nil, errors.New("the native cookie key is locked; unlock the Linux login keyring")
	}
	if len(unlocked) != 1 {
		return nil, errors.New("expected one native Chromium cookie key in the host login keyring")
	}
	var output dbus.Variant
	var session dbus.ObjectPath
	if err := service.CallWithContext(ctx, "org.freedesktop.Secret.Service.OpenSession", 0, "plain", dbus.MakeVariant("")).Store(&output, &session); err != nil {
		return nil, errors.New("cannot open a private host keyring session")
	}
	defer connection.Object("org.freedesktop.secrets", session).Call("org.freedesktop.Secret.Session.Close", dbus.FlagNoReplyExpected)
	var secret struct {
		Session     dbus.ObjectPath
		Parameters  []byte
		Value       []byte
		ContentType string
	}
	item := connection.Object("org.freedesktop.secrets", unlocked[0])
	if err := item.CallWithContext(ctx, "org.freedesktop.Secret.Item.GetSecret", 0, session).Store(&secret); err != nil {
		return nil, errors.New("cannot read the native cookie key")
	}
	if secret.Session != session || len(secret.Parameters) != 0 || len(secret.Value) == 0 || len(secret.Value) > 4096 || bytes.ContainsAny(secret.Value, "\x00\r\n") {
		return nil, errors.New("native cookie key has an unsupported format")
	}
	return secret.Value, nil
}
