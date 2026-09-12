package usercontext

import (
	"strings"
	"testing"
)

func TestImageAccountKeepsTheHostHome(t *testing.T) {
	for _, home := range []string{"/home/alice", "/Users/alice", "/srv/Team A/personal"} {
		t.Run(home, func(t *testing.T) {
			account := Account{Name: "work", Group: "staff", UID: 1007, GID: 20, Home: home}
			if err := account.Validate(); err != nil {
				t.Fatal(err)
			}
			args := account.BuildArgs()
			if args["USER_HOME"] != home || args["USER_NAME"] != "work" || args["USER_UID"] != "1007" || args["USER_GID"] != "20" {
				t.Fatalf("build identity = %#v", args)
			}
			if err := account.CheckLabel(args["COOPER_ACCOUNT"]); err != nil {
				t.Fatal(err)
			}
			other := account
			other.Home = "/home/another-account"
			if err := other.CheckLabel(account.Label()); err == nil || !strings.Contains(err.Error(), "cooper build") {
				t.Fatalf("mismatched home error = %v", err)
			}
		})
	}
}

func TestImageAccountRejectsOldImagesAndDifferentOwners(t *testing.T) {
	account := Account{Name: "alice", Group: "alice", UID: 1000, GID: 1000, Home: "/home/alice"}
	other := account
	other.UID = 1001
	for _, label := range []string{"", "<no value>", "{}", other.Label()} {
		if err := account.CheckLabel(label); err == nil || !strings.Contains(err.Error(), "cooper build") {
			t.Fatalf("CheckLabel(%q) error = %v", label, err)
		}
	}
}

func TestAccountRejectsUnsafePasswordDatabaseValues(t *testing.T) {
	for _, home := range []string{"/", "relative", "/home/alice:other", "/home/alice\nroot"} {
		account := Account{Name: "alice", Group: "alice", UID: 1000, GID: 1000, Home: home}
		if err := account.Validate(); err == nil {
			t.Fatalf("accepted home %q", home)
		}
	}
}
