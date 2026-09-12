package vm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeMetadataRoundTrip(t *testing.T) {
	t.Parallel()
	cooperDir := t.TempDir()
	runtimeID := "test-vm-project-codex-aabbccddeeff"
	runtimeDir := RuntimeDir(cooperDir, runtimeID)
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	want := RuntimeMetadata{
		Schema: runtimeMetadataSchema, RuntimeID: runtimeID, ToolName: "codex",
		WorkspaceDir: "/work/Project Name:世界", ImageRef: "test-cooper-cli-codex",
		ImageID: "sha256:" + strings.Repeat("a", 64), Depth: 1,
		CPUs: 4, MemoryMiB: 4096, DiskGiB: 16, ClipboardMode: "shim",
		MountPlanSHA256: strings.Repeat("b", 64),
	}
	if err := writeJSON(filepath.Join(runtimeDir, "runtime.json"), want, 0o444); err != nil {
		t.Fatal(err)
	}
	got, err := loadRuntimeMetadata(cooperDir, runtimeID)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("runtime metadata = %#v, want %#v", got, want)
	}
}

func TestRuntimeMetadataRejectsUnsafeValues(t *testing.T) {
	t.Parallel()
	valid := RuntimeMetadata{
		Schema: runtimeMetadataSchema, RuntimeID: "test-vm-project-codex-aabbccddeeff",
		ToolName: "codex", WorkspaceDir: "/work/project", ImageRef: "test-image",
		ImageID: "sha256:" + strings.Repeat("a", 64), Depth: 1,
		CPUs: 4, MemoryMiB: 4096, DiskGiB: 16, ClipboardMode: "shim",
		MountPlanSHA256: strings.Repeat("b", 64),
	}
	tests := []struct {
		name   string
		change func(*RuntimeMetadata)
	}{
		{name: "schema", change: func(value *RuntimeMetadata) { value.Schema++ }},
		{name: "identity", change: func(value *RuntimeMetadata) { value.RuntimeID = "other-vm" }},
		{name: "relative workspace", change: func(value *RuntimeMetadata) { value.WorkspaceDir = "work" }},
		{name: "image digest", change: func(value *RuntimeMetadata) { value.ImageID = "latest" }},
		{name: "depth", change: func(value *RuntimeMetadata) { value.Depth = 3 }},
		{name: "resources", change: func(value *RuntimeMetadata) { value.MemoryMiB = 1024 }},
		{name: "clipboard mode", change: func(value *RuntimeMetadata) { value.ClipboardMode = "secret" }},
		{name: "mount plan", change: func(value *RuntimeMetadata) { value.MountPlanSHA256 = "bad" }},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value := valid
			test.change(&value)
			if err := value.validate(valid.RuntimeID); err == nil {
				t.Fatal("unsafe runtime metadata passed validation")
			}
		})
	}
}

func TestRuntimeMetadataReuseRequiresExactInputs(t *testing.T) {
	t.Parallel()
	metadata := RuntimeMetadata{
		Schema: runtimeMetadataSchema, RuntimeID: "test-vm-project-codex-aabbccddeeff",
		ToolName: "codex", WorkspaceDir: "/work/project", ImageRef: "test-image",
		ImageID: "sha256:" + strings.Repeat("a", 64), Depth: 1,
		CPUs: 4, MemoryMiB: 4096, DiskGiB: 16, ClipboardMode: "shim",
		MountPlanSHA256: strings.Repeat("b", 64),
	}
	request := metadata.startRequest()
	if !metadata.matches(request, 1, metadata.ImageID, metadata.MountPlanSHA256) {
		t.Fatal("exact launch inputs did not match")
	}
	request.MemoryMiB++
	if metadata.matches(request, 1, metadata.ImageID, metadata.MountPlanSHA256) {
		t.Fatal("changed VM resources matched reusable metadata")
	}
	request = metadata.startRequest()
	request.ImageRef = "renamed-test-image"
	if metadata.matches(request, 1, metadata.ImageID, metadata.MountPlanSHA256) {
		t.Fatal("changed image reference matched reusable metadata")
	}
}

func TestLoadRuntimeMetadataRejectsUnknownField(t *testing.T) {
	t.Parallel()
	cooperDir := t.TempDir()
	runtimeID := "test-vm-project-codex-aabbccddeeff"
	runtimeDir := RuntimeDir(cooperDir, runtimeID)
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"schema":2,"runtime_id":"` + runtimeID + `","tool":"codex","workspace":"/work","image_ref":"test","image_id":"sha256:` + strings.Repeat("a", 64) + `","depth":1,"cpus":4,"memory_mib":4096,"disk_gib":16,"clipboard_mode":"shim","mount_plan_sha256":"` + strings.Repeat("b", 64) + `","unexpected":true}`
	if err := os.WriteFile(filepath.Join(runtimeDir, "runtime.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRuntimeMetadata(cooperDir, runtimeID); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("loadRuntimeMetadata() error = %v", err)
	}
}
