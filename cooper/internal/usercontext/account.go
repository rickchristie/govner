// Package usercontext defines the host account that an agent image represents.
// Account identity is a build input. State roots remain launch inputs, so a
// path override never requires a different image or changes file ownership.
package usercontext

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const ImageLabel = "cooper.account"

// Account records the logical home separately from the login name. A Linux
// account can have a macOS-style or administrator-selected home directory.
type Account struct {
	Name  string `json:"name"`
	Group string `json:"group"`
	UID   int    `json:"uid"`
	GID   int    `json:"gid"`
	Home  string `json:"home"`
}

var accountName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*\$?$`)

// Current reads the process IDs and password database. USER and LOGNAME can
// be absent or stale, so they are not used to select an account.
func Current() (Account, error) {
	entry, err := user.LookupId(strconv.Itoa(os.Getuid()))
	if err != nil {
		return Account{}, fmt.Errorf("read host account: %w", err)
	}
	group, err := user.LookupGroupId(strconv.Itoa(os.Getgid()))
	if err != nil {
		return Account{}, fmt.Errorf("read host group: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Account{}, fmt.Errorf("read host home: %w", err)
	}
	account := Account{Name: entry.Username, Group: group.Name, UID: os.Getuid(), GID: os.Getgid(), Home: home}
	return account, account.Validate()
}

func (a Account) Validate() error {
	if !accountName.MatchString(a.Name) || !accountName.MatchString(a.Group) {
		return fmt.Errorf("host account %q or group %q is not a supported Linux account name", a.Name, a.Group)
	}
	if a.UID <= 0 || a.GID <= 0 {
		return fmt.Errorf("Cooper agent images require a non-root host UID and GID")
	}
	if !filepath.IsAbs(a.Home) || filepath.Clean(a.Home) != a.Home || a.Home == "/" || strings.ContainsAny(a.Home, "\x00\r\n:") {
		return fmt.Errorf("host home %q must be a clean absolute directory path", a.Home)
	}
	return nil
}

// Label is safe to pass as one Docker build argument. It contains no secrets.
func (a Account) Label() string {
	data, _ := json.Marshal(a)
	return string(data)
}

func (a Account) BuildArgs() map[string]string {
	return map[string]string{
		"USER_NAME": a.Name, "USER_GROUP": a.Group,
		"USER_UID": strconv.Itoa(a.UID), "USER_GID": strconv.Itoa(a.GID),
		"USER_HOME": a.Home, "COOPER_ACCOUNT": a.Label(),
	}
}

// CheckLabel rejects an old image or another account before state is mounted.
func (a Account) CheckLabel(value string) error {
	var built Account
	if err := json.Unmarshal([]byte(value), &built); err != nil {
		return fmt.Errorf("agent image has no supported host account record; run 'cooper build'")
	}
	if built != a {
		return fmt.Errorf("agent image account is %s (%d:%d, %s); current account is %s (%d:%d, %s); run 'cooper build'", built.Name, built.UID, built.GID, built.Home, a.Name, a.UID, a.GID, a.Home)
	}
	return nil
}
