package vme2e

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/testdocker"
)

func (f *developmentFixture) desktopNetwork() {
	diagnostic, err := f.manager.GuestDiagnostic(f.ctx, f.state)
	if err != nil {
		f.t.Fatal(err)
	}
	if len(diagnostic.ExternalInterfaces) != 0 || len(diagnostic.DefaultRoutes) != 0 {
		f.t.Fatalf("guest has an external network: %#v", diagnostic)
	}
	assertHostBoundary(f.t, f.state, f.run.Workspace, f.home)
	target, err := testdocker.StartHTTPSTarget("vm-allowed.cooper.test", "vm-blocked.cooper.test")
	if err != nil {
		f.t.Fatal(err)
	}
	defer target.Remove()
	f.run.Objects = append(f.run.Objects, developmentObject{Type: "container", Name: target.ContainerName, ID: strings.TrimSpace(f.command("docker", "inspect", "--format", "{{.Id}}", target.ContainerName))})
	f.saveRun()
	for _, domain := range target.Domains {
		f.command("docker", "exec", target.ContainerName, "bash", "-ec", httpsTargetProbe(domain, ""))
	}
	certificate := f.command("docker", "exec", target.ContainerName, "cat", "/tmp/target.crt")
	writeFile(f.t, filepath.Join(f.run.Workspace, "desktop-test-target.crt"), certificate)
	script := fmt.Sprintf(`set -eu
export CURL_CA_BUNDLE="$PWD/desktop-test-target.crt"
test "$(curl -fsS --max-time 10 https://vm-allowed.cooper.test/)" = ok
if curl -kfsS --max-time 5 https://vm-blocked.cooper.test/ >/dev/null 2>&1; then exit 31; fi
if curl -fsS --connect-timeout 2 --max-time 4 --noproxy '*' --resolve vm-allowed.cooper.test:443:%s https://vm-allowed.cooper.test/ >/dev/null 2>&1; then exit 32; fi
if curl --noproxy '*' -fsS --connect-timeout 2 --max-time 4 http://1.1.1.1/ >/dev/null 2>&1; then exit 33; fi
printf DESKTOP_NETWORK_OK
`, target.IP)
	f.t.Log(execVM(f.t, f.manager, f.state, script))
}
