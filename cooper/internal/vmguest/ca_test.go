package vmguest

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestVerifyCADigest(t *testing.T) {
	t.Parallel()
	data := []byte("public Cooper test CA")
	digest := sha256.Sum256(data)
	want := hex.EncodeToString(digest[:])
	if err := verifyCADigest(data, want); err != nil {
		t.Fatal(err)
	}
	if err := verifyCADigest(data, strings.Repeat("0", 64)); err == nil {
		t.Fatal("verifyCADigest accepted changed CA data")
	}
}
