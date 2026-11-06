package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestImageBuildInputs(t *testing.T) {
	dir := t.TempDir()
	dockerfile := filepath.Join(dir, "Dockerfile")
	source := filepath.Join(dir, "source")
	for path, data := range map[string]string{dockerfile: "FROM scratch\nCOPY source /source\n", source: "one"} {
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	inputs := func(args map[string]string) string {
		t.Helper()
		value, err := imageBuildInputs(dockerfile, dir, args)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	args := map[string]string{"USER_UID": "1000", "USER_GID": "1000"}
	before := inputs(args)
	if got := inputs(map[string]string{"USER_GID": "1000", "USER_UID": "1000"}); got != before {
		t.Fatal("argument order changed the input record")
	}
	stamp := time.Unix(123, 0)
	if err := os.Chtimes(source, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if inputs(args) != before {
		t.Fatal("file time changed the input record")
	}
	for _, test := range []struct {
		name   string
		change func() error
	}{
		{"account", func() error { args["USER_UID"] = "1001"; return nil }},
		{"file contents", func() error { return os.WriteFile(source, []byte("two"), 0644) }},
		{"file mode", func() error { return os.Chmod(source, 0755) }},
		{"new file", func() error { return os.WriteFile(filepath.Join(dir, "extra"), []byte("new"), 0644) }},
		{"removed file", func() error { return os.Remove(filepath.Join(dir, "extra")) }},
		{"Docker ignore rules", func() error { return os.WriteFile(filepath.Join(dir, ".dockerignore"), []byte("source\n"), 0644) }},
		{"platform", func() error { t.Setenv("DOCKER_DEFAULT_PLATFORM", "test/platform"); return nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := inputs(args)
			if err := test.change(); err != nil {
				t.Fatal(err)
			}
			if inputs(args) == before {
				t.Fatal("changed build input did not change the record")
			}
		})
	}
	if err := os.Symlink(source, filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := imageBuildInputs(dockerfile, dir, args); err == nil {
		t.Fatal("symlink context must use Docker's own cache checks")
	}
}

func TestPrepareImageBuildLeavesCustomAndUnsupportedInputsToDocker(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(file, []byte("FROM ${BASE}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{GetImageCLI("custom"), GetImageBase()} {
		args, inputs, reused, err := prepareImageBuild(name, file, dir, nil, false, func(string) {})
		if err != nil || reused || inputs != "" || !strings.HasPrefix(strings.Join(args, " "), "build ") {
			t.Fatalf("%s did not use Docker's own build: %v, %v, %v", name, args, reused, err)
		}
	}
}

func TestBuildParentImagesRejectsUnresolvedInputs(t *testing.T) {
	for _, input := range []string{"FROM", "FROM ${BASE}", "FROM --platform=linux/amd64 base", "FROM base AS"} {
		if _, err := buildParentImages(input); err == nil {
			t.Fatalf("accepted unsupported instruction %q", input)
		}
	}
	parents, err := buildParentImages("FROM scratch AS first\nFROM first AS second\nFROM second\n")
	if err != nil || len(parents) != 0 {
		t.Fatalf("stage names were treated as image references: %v, %v", parents, err)
	}
}
