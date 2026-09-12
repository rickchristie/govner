package workload

import (
	"fmt"

	"github.com/rickchristie/govner/cooper/internal/config"
)

// RuntimeEnvironment returns the non-secret environment that both execution
// back ends give to a selected agent. proxyHost is the only boundary-specific
// value.
func RuntimeEnvironment(cfg *config.Config, proxyHost, internalNetwork string) []EnvVar {
	proxyURL := fmt.Sprintf("http://%s:%d", proxyHost, cfg.ProxyPort)
	return []EnvVar{
		{Name: "HTTP_PROXY", Value: proxyURL},
		{Name: "HTTPS_PROXY", Value: proxyURL},
		{Name: "NO_PROXY", Value: "localhost,127.0.0.1"},
		// Docker client proxy settings inject both name forms into child
		// containers. Set both forms here so a nested barrel cannot keep its
		// outer VM proxy and bypass the nested Cooper proxy.
		{Name: "http_proxy", Value: proxyURL},
		{Name: "https_proxy", Value: proxyURL},
		{Name: "no_proxy", Value: "localhost,127.0.0.1"},
		{Name: "TZ", Value: ":" + TimezoneContainerPath},
		{Name: "COOPER_PROXY_HOST", Value: proxyHost},
		{Name: "COOPER_INTERNAL_NETWORK", Value: internalNetwork},
		{Name: "DISPLAY", Value: "127.0.0.1:99"},
		{Name: "XAUTHORITY", Value: "/var/lib/cooper/clipboard/xauth"},
		{Name: "COOPER_CLIPBOARD_DISPLAY", Value: "127.0.0.1:99"},
		{Name: "COOPER_CLIPBOARD_XAUTHORITY", Value: "/var/lib/cooper/clipboard/xauth"},
		{Name: "PLAYWRIGHT_BROWSERS_PATH", Value: PlaywrightCacheDir},
		{Name: "NPM_CONFIG_CACHE", Value: NPMCacheDir},
		{Name: "PIP_CACHE_DIR", Value: PIPCacheDir},
		{Name: "COOPER_CLIPBOARD_ENABLED", Value: "1"},
		{Name: "COOPER_CLIPBOARD_BRIDGE_URL", Value: fmt.Sprintf("http://127.0.0.1:%d", cfg.BridgePort)},
		{Name: "COOPER_CLIPBOARD_TOKEN_FILE", Value: "/etc/cooper/clipboard-token"},
		{Name: "COOPER_CLIPBOARD_SHIMS", Value: "xclip,xsel"},
	}
}

// RenderEnvironment renders values only at the process boundary.
func RenderEnvironment(values []EnvVar) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value.Unset {
			result = append(result, value.Name)
			continue
		}
		result = append(result, value.Name+"="+value.Value)
	}
	return result
}
