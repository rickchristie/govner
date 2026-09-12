package workload

import (
	"reflect"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestRuntimeEnvironmentUsesBackendProxyOnly(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	environment := RuntimeEnvironment(cfg, "172.30.0.1", "cooper-control")
	rendered := RenderEnvironment(environment)
	for _, want := range []string{
		"HTTP_PROXY=http://172.30.0.1:3128",
		"HTTPS_PROXY=http://172.30.0.1:3128",
		"NO_PROXY=localhost,127.0.0.1",
		"http_proxy=http://172.30.0.1:3128",
		"https_proxy=http://172.30.0.1:3128",
		"no_proxy=localhost,127.0.0.1",
		"TZ=:/run/cooper/host-localtime",
		"COOPER_INTERNAL_NETWORK=cooper-control",
		"COOPER_CLIPBOARD_BRIDGE_URL=http://127.0.0.1:4343",
	} {
		found := false
		for _, value := range rendered {
			if value == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("environment does not contain %q: %#v", want, rendered)
		}
	}
	if !reflect.DeepEqual(rendered, RenderEnvironment(environment)) {
		t.Fatal("environment rendering is not deterministic")
	}
}
