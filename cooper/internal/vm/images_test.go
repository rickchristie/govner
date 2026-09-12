package vm

import (
	"strings"
	"testing"
)

func TestInfrastructureDockerfilesAreFailClosed(t *testing.T) {
	t.Parallel()
	supervisor := supervisorDockerfile()
	for _, want := range []string{
		supervisorBaseImage, ubuntuSnapshot, "APT::Update::Error-Mode=any",
		"Acquire::Retries", "Acquire::https::Timeout",
		"Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg",
		"ca-certificates=20260601~24.04.1", "libssl3t64=3.0.13-0ubuntu3.12",
		"openssl=3.0.13-0ubuntu3.12", "qemu-system-x86=1:8.2.2+ds-0ubuntu1.17",
		"qemu-utils=1:8.2.2+ds-0ubuntu1.17",
		"rm /etc/apt/apt.conf.d/00cooper-signed-snapshot-bootstrap",
		"test -s /etc/ssl/certs/ca-certificates.crt",
		"COPY virtiofsd /usr/libexec/virtiofsd", "virtiofsd 1.14.0", "__vm-supervisor",
	} {
		if !strings.Contains(supervisor, want) {
			t.Fatalf("supervisor Dockerfile does not contain %q", want)
		}
	}
	snapshotSource := strings.Index(supervisor, "URIs: https://snapshot.ubuntu.com")
	firstUpdate := strings.Index(supervisor, "apt-get update")
	if snapshotSource < 0 || firstUpdate < snapshotSource {
		t.Fatalf("supervisor reads a moving package index before the fixed snapshot:\n%s", supervisor)
	}
	for _, unwanted := range []string{"archive.ubuntu.com", "virtiofsd=1.10.0-1", "libssl3t64=3.0.13-0ubuntu3.15"} {
		if strings.Contains(supervisor, unwanted) {
			t.Fatalf("supervisor Dockerfile contains unpinned or unused package input %q:\n%s", unwanted, supervisor)
		}
	}
	relay := relayDockerfile()
	if strings.Contains(relay, "qemu") || !strings.Contains(relay, "FROM scratch") || strings.Contains(relay, "RUN ") || !strings.Contains(relay, "USER 65534:65534") || !strings.Contains(relay, "__vm-relay") {
		t.Fatalf("unsafe relay Dockerfile:\n%s", relay)
	}
}
