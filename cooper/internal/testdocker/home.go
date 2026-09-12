package testdocker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/usercontext"
)

// UseHome builds a small account layer over the shared test images. Each test
// can use an isolated host home without relaxing production identity checks or
// downloading the tools again. The package Docker lock serializes tag changes.
func UseHome(home string) (func(), error) {
	account, err := usercontext.Current()
	if err != nil {
		return nil, err
	}
	account.Home = home
	if err := account.Validate(); err != nil {
		return nil, err
	}
	exists, err := docker.ImageExists(docker.GetImageBase())
	if err != nil {
		return nil, err
	}
	if !exists {
		// A custom-prefix driver can build its images after it is created.
		return func() {}, nil
	}
	images, err := docker.ListCLIImages()
	if err != nil {
		return nil, err
	}
	images = append(images, docker.GetImageBase())
	contextDir, err := os.MkdirTemp("", "cooper-test-account-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(contextDir)
	dockerfile := filepath.Join(contextDir, "Dockerfile")
	const recipe = `ARG SOURCE_IMAGE
FROM ${SOURCE_IMAGE}
USER root
ARG TEST_HOME
ARG COOPER_ACCOUNT
RUN usermod --home "$TEST_HOME" "$COOPER_USER_NAME" \
    && install -d -m 0700 -o "$COOPER_USER_NAME" -g "$COOPER_USER_GROUP" "$TEST_HOME" \
    && printf '%s\n' "$COOPER_ACCOUNT" > /etc/cooper/account.json
ENV HOME=${TEST_HOME}
LABEL cooper.account="${COOPER_ACCOUNT}"
USER ${COOPER_USER_NAME}
`
	if err := os.WriteFile(dockerfile, []byte(recipe), 0o600); err != nil {
		return nil, err
	}
	type savedImage struct{ name, id, variant string }
	var saved []savedImage
	restore := func() {
		for _, image := range saved {
			_ = exec.Command("docker", "tag", image.id, image.name).Run()
		}
		for _, image := range saved {
			if image.variant != "" {
				_ = exec.Command("docker", "image", "rm", image.variant).Run()
			}
		}
	}
	for _, image := range images {
		output, err := exec.Command("docker", "image", "inspect", "--format", `{{.Id}}{{println}}{{index .Config.Labels "cooper.account"}}`, image).Output()
		if err != nil {
			restore()
			return nil, err
		}
		id, label, _ := strings.Cut(strings.TrimSpace(string(output)), "\n")
		if label == account.Label() {
			continue
		}
		saved = append(saved, savedImage{name: image, id: id})
		args := map[string]string{"SOURCE_IMAGE": id, "TEST_HOME": home, "COOPER_ACCOUNT": account.Label()}
		if err := docker.BuildImage(image, dockerfile, contextDir, args, false); err != nil {
			restore()
			return nil, fmt.Errorf("build private test home: %w", err)
		}
		output, err = exec.Command("docker", "image", "inspect", "--format", "{{.Id}}", image).Output()
		if err != nil {
			restore()
			return nil, err
		}
		saved[len(saved)-1].variant = strings.TrimSpace(string(output))
	}
	return restore, nil
}
