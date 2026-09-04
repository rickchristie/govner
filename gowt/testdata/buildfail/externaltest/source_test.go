package externaltest_test

import "testing"

func TestBrokenExternal(t *testing.T) {
	_ = missingExternalTestSymbol
}
