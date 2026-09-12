package vmpayload

import (
	"bytes"
	"debug/elf"
	"runtime"
	"testing"
)

func TestEmbeddedVirtioFSDIsStaticLinuxAMD64ELF(t *testing.T) {
	t.Parallel()
	payload, err := VirtioFSD()
	if err != nil {
		t.Fatal(err)
	}
	file, err := elf.NewFile(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("open embedded virtiofsd ELF: %v", err)
	}
	defer file.Close()
	if file.Machine != elf.EM_X86_64 || file.Class != elf.ELFCLASS64 {
		t.Fatalf("embedded virtiofsd is %s/%s; want Linux x86-64", file.Machine, file.Class)
	}
	needed, err := file.DynString(elf.DT_NEEDED)
	if err == nil && len(needed) != 0 {
		t.Fatalf("embedded virtiofsd needs dynamic libraries: %v", needed)
	}
	if runtime.GOARCH != "amd64" {
		t.Skip("the first VM schema supports x86-64 only")
	}
}
