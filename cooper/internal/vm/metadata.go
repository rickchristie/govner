package vm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const runtimeMetadataSchema = 2

// RuntimeMetadata is the host-owned restart contract for one VM. It contains
// no credential or environment value.
type RuntimeMetadata struct {
	Schema          int    `json:"schema"`
	RuntimeID       string `json:"runtime_id"`
	ToolName        string `json:"tool"`
	WorkspaceDir    string `json:"workspace"`
	ImageRef        string `json:"image_ref"`
	ImageID         string `json:"image_id"`
	Depth           int    `json:"depth"`
	CPUs            int    `json:"cpus"`
	MemoryMiB       int    `json:"memory_mib"`
	DiskGiB         int    `json:"disk_gib"`
	ClipboardMode   string `json:"clipboard_mode"`
	MountPlanSHA256 string `json:"mount_plan_sha256"`
}

func (m RuntimeMetadata) validate(expectedID string) error {
	if m.Schema != runtimeMetadataSchema {
		return fmt.Errorf("unsupported VM runtime metadata schema %d", m.Schema)
	}
	if m.RuntimeID != expectedID || cleanNamePart(m.RuntimeID) != m.RuntimeID {
		return errors.New("VM runtime metadata identity does not match its directory")
	}
	if strings.TrimSpace(m.ToolName) == "" || !filepath.IsAbs(m.WorkspaceDir) || strings.TrimSpace(m.ImageRef) == "" {
		return errors.New("VM runtime metadata has missing launch values")
	}
	if !validImageID(m.ImageID) {
		return errors.New("VM runtime metadata has an invalid image ID")
	}
	if len(m.MountPlanSHA256) != 64 {
		return errors.New("VM runtime metadata has an invalid mount-plan digest")
	}
	for _, character := range m.MountPlanSHA256 {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return errors.New("VM runtime metadata has an invalid mount-plan digest")
		}
	}
	if m.Depth < 1 || m.Depth > 2 {
		return fmt.Errorf("VM runtime metadata depth %d is outside 1-2", m.Depth)
	}
	request := StartRequest{CPUs: m.CPUs, MemoryMiB: m.MemoryMiB, DiskGiB: m.DiskGiB, ClipboardMode: m.ClipboardMode}
	if err := validateResources(request); err != nil {
		return err
	}
	return nil
}

func (m RuntimeMetadata) matches(request StartRequest, depth int, imageID, mountDigest string) bool {
	return m.RuntimeID == request.RuntimeID &&
		m.ToolName == request.ToolName &&
		m.WorkspaceDir == request.WorkspaceDir &&
		m.ImageRef == request.ImageRef &&
		m.ImageID == imageID &&
		m.Depth == depth &&
		m.CPUs == request.CPUs &&
		m.MemoryMiB == request.MemoryMiB &&
		m.DiskGiB == request.DiskGiB &&
		m.ClipboardMode == request.ClipboardMode &&
		m.MountPlanSHA256 == mountDigest
}

func loadRuntimeMetadata(cooperDir, runtimeID string) (RuntimeMetadata, error) {
	path := filepath.Join(RuntimeDir(cooperDir, runtimeID), "runtime.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return RuntimeMetadata{}, fmt.Errorf("read VM runtime metadata: %w", err)
	}
	var metadata RuntimeMetadata
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return RuntimeMetadata{}, fmt.Errorf("decode VM runtime metadata: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("unexpected data after JSON object")
		}
		return RuntimeMetadata{}, fmt.Errorf("decode VM runtime metadata: %w", err)
	}
	if err := metadata.validate(runtimeID); err != nil {
		return RuntimeMetadata{}, err
	}
	return metadata, nil
}

func (m RuntimeMetadata) startRequest() StartRequest {
	return StartRequest{
		WorkspaceDir: m.WorkspaceDir, ToolName: m.ToolName, ImageRef: m.ImageRef,
		RuntimeID: m.RuntimeID, CPUs: m.CPUs, MemoryMiB: m.MemoryMiB, DiskGiB: m.DiskGiB,
		ClipboardMode: m.ClipboardMode,
	}
}
