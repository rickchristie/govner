package vm

import "testing"

func TestValidateHostIDsRejectsRootIdentity(t *testing.T) {
	t.Parallel()
	for _, identity := range []struct {
		uid int
		gid int
	}{
		{uid: 0, gid: 1000},
		{uid: 1000, gid: 0},
	} {
		if err := validateHostIDs(identity.uid, identity.gid); err == nil {
			t.Fatalf("validateHostIDs(%d, %d) accepted a root identity", identity.uid, identity.gid)
		}
	}
	if err := validateHostIDs(1000, 1000); err != nil {
		t.Fatal(err)
	}
}
