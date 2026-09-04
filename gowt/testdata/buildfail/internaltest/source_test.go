package internaltest

import "testing"

func TestBrokenInternal(t *testing.T) {
	_ = missingInternalTestSymbol
}
