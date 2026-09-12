package vm

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/vmpayload"
)

const (
	supervisorBaseImage       = "ubuntu@sha256:186072bba1b2f436cbb91ef2567abca677337cfc786c86e107d25b7072feef0c"
	ubuntuSnapshot            = "20260801T000000Z"
	infrastructureImageSchema = "2"
)

// InfrastructureImages owns the trusted helper images.
type InfrastructureImages struct {
	CooperDir  string
	Prefix     string
	Runner     CommandRunner
	Out        io.Writer
	Executable string
}

// Ensure builds schema-versioned images when they do not exist. The context
// contains only the current Cooper binary and reviewed Dockerfiles.
func (b InfrastructureImages) Ensure(ctx context.Context) error {
	runner := runnerOrSystem(b.Runner)
	executable, err := b.executable()
	if err != nil {
		return err
	}
	binarySHA, err := digestFile(executable)
	if err != nil {
		return fmt.Errorf("hash Cooper VM helper: %w", err)
	}
	supervisor := SupervisorImageName(b.Prefix)
	relay := RelayImageName(b.Prefix)
	for _, image := range []string{supervisor, relay} {
		label, inspectErr := runner.Output(ctx, "docker", "image", "inspect", "--format", `{{index .Config.Labels "cooper.vm.binary-sha"}}/{{index .Config.Labels "cooper.vm.image-schema"}}`, image)
		if inspectErr == nil && strings.TrimSpace(string(label)) == binarySHA+"/"+infrastructureImageSchema {
			continue
		}
		if err := b.build(ctx, runner, image, image == supervisor, binarySHA); err != nil {
			return err
		}
	}
	return nil
}

func (b InfrastructureImages) build(ctx context.Context, runner CommandRunner, image string, supervisor bool, binarySHA string) error {
	executable, err := b.executable()
	if err != nil {
		return err
	}
	contextDir := filepath.Join(b.CooperDir, "vm", "build", AssetSchema)
	if err := os.MkdirAll(contextDir, 0o700); err != nil {
		return fmt.Errorf("create VM image context: %w", err)
	}
	if err := copyFileAtomic(executable, filepath.Join(contextDir, "cooper"), 0o555); err != nil {
		return fmt.Errorf("stage Cooper VM helper: %w", err)
	}
	virtiofsd, err := vmpayload.VirtioFSD()
	if err != nil {
		return err
	}
	if err := writeFileAtomic(virtiofsd, filepath.Join(contextDir, "virtiofsd"), 0o555); err != nil {
		return fmt.Errorf("stage Cooper VM virtiofsd: %w", err)
	}
	dockerfile := relayDockerfile()
	if supervisor {
		dockerfile = supervisorDockerfile()
	}
	dockerfilePath := filepath.Join(contextDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0o600); err != nil {
		return fmt.Errorf("write VM infrastructure Dockerfile: %w", err)
	}
	output := b.Out
	if output == nil {
		output = os.Stderr
	}
	args := []string{"build", "--pull=false"}
	proxyArgs, err := docker.NestedBuildProxyArgs()
	if err != nil {
		return err
	}
	args = append(args, proxyArgs...)
	args = append(args,
		"--label", "cooper.vm.schema="+AssetSchema,
		"--label", "cooper.vm.image-schema="+infrastructureImageSchema,
		"--label", "cooper.vm.binary-sha="+binarySHA,
		"-t", image, contextDir)
	if err := runner.Run(ctx, nil, output, output, "docker", args...); err != nil {
		return fmt.Errorf("build VM infrastructure image %s: %w", image, err)
	}
	return nil
}

func (b InfrastructureImages) executable() (string, error) {
	if strings.TrimSpace(b.Executable) != "" {
		return filepath.Abs(b.Executable)
	}
	return currentExecutable()
}

func supervisorDockerfile() string {
	return `FROM ` + supervisorBaseImage + `
ENV DEBIAN_FRONTEND=noninteractive
# The minimal pinned base has no CA bundle. During this one bootstrap, APT
# still verifies Ubuntu's signed InRelease files and package hashes with the
# pinned archive keyring. TLS peer verification becomes mandatory as soon as
# the verified ca-certificates package is installed.
RUN printf '%s\n' \
      'Acquire::Retries "10";' \
      'Acquire::http::Timeout "30";' \
      'Acquire::https::Timeout "30";' \
      > /etc/apt/apt.conf.d/99cooper-network \
    && printf '%s\n' \
      'Types: deb' \
      'URIs: https://snapshot.ubuntu.com/ubuntu/` + ubuntuSnapshot + `' \
      'Suites: noble noble-updates noble-security' \
      'Components: main universe' \
      'Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg' \
      > /etc/apt/sources.list.d/ubuntu.sources \
    && printf '%s\n' \
      'Acquire::https::Verify-Peer "false";' \
      > /etc/apt/apt.conf.d/00cooper-signed-snapshot-bootstrap \
    && apt-get update -o APT::Update::Error-Mode=any -o Acquire::Check-Valid-Until=false \
    && apt-get install -y --no-install-recommends \
      ca-certificates=20260601~24.04.1 \
      cloud-image-utils=0.33-1 \
      libssl3t64=3.0.13-0ubuntu3.12 \
      openssl=3.0.13-0ubuntu3.12 \
      qemu-system-x86=1:8.2.2+ds-0ubuntu1.17 \
      qemu-utils=1:8.2.2+ds-0ubuntu1.17 \
    && rm /etc/apt/apt.conf.d/00cooper-signed-snapshot-bootstrap \
    && test -s /etc/ssl/certs/ca-certificates.crt \
    && rm -rf /var/lib/apt/lists/*
COPY cooper /usr/local/bin/cooper
COPY virtiofsd /usr/libexec/virtiofsd
RUN chmod 0555 /usr/local/bin/cooper /usr/libexec/virtiofsd \
	&& test "$(/usr/libexec/virtiofsd --version)" = "virtiofsd ` + vmpayload.VirtioFSDVersion + `"
ENTRYPOINT ["/usr/local/bin/cooper", "__vm-supervisor"]
`
}

func relayDockerfile() string {
	return `FROM ` + supervisorBaseImage + ` AS runtime
FROM scratch
COPY --from=runtime /lib/x86_64-linux-gnu/libc.so.6 /lib/x86_64-linux-gnu/libc.so.6
COPY --from=runtime /lib64/ld-linux-x86-64.so.2 /lib64/ld-linux-x86-64.so.2
COPY cooper /usr/local/bin/cooper
USER 65534:65534
ENTRYPOINT ["/usr/local/bin/cooper", "__vm-relay"]
`
}
