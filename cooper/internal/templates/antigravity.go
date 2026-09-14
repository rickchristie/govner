package templates

import (
	"fmt"

	"github.com/rickchristie/govner/cooper/internal/antigravity"
	"github.com/rickchristie/govner/cooper/internal/config"
)

func renderAntigravityInstall(cfg *config.Config, version string) (string, error) {
	// The native CLI embeds a Playwright Go client. Its driver must match that
	// client, not the workspace's Playwright version. Review this dependency
	// when accepting another native release; do not guess on a moving Latest.
	if version != "1.2.2" {
		return "", fmt.Errorf("Antigravity %q has no reviewed browser driver; supported native release: 1.2.2", version)
	}
	releases := map[string]antigravity.Release{}
	for _, tool := range cfg.AITools {
		if tool.Name != "antigravity" {
			continue
		}
		for _, release := range tool.AntigravityReleases {
			if err := release.Validate(); err != nil {
				return "", err
			}
			if release.Version != version {
				return "", fmt.Errorf("Antigravity release record does not match selected version")
			}
			if _, exists := releases[release.Arch]; exists {
				return "", fmt.Errorf("duplicate Antigravity platform record")
			}
			releases[release.Arch] = release
		}
	}
	amd, arm := releases["amd64"], releases["arm64"]
	if amd.URL == "" || arm.URL == "" {
		return "", fmt.Errorf("Antigravity build needs frozen amd64 and arm64 release records; run cooper build to resolve them")
	}
	return fmt.Sprintf(`ARG TARGETARCH
RUN set -eu; \
    agy_arch="${TARGETARCH:-$(uname -m)}"; \
    case "$agy_arch" in \
      amd64|x86_64) agy_url='%s'; agy_digest='%s' ;; \
      arm64|aarch64) agy_url='%s'; agy_digest='%s' ;; \
      *) echo "unsupported Antigravity architecture: $agy_arch" >&2; exit 1 ;; \
    esac; \
    agy_archive=$(mktemp); \
    curl --fail --silent --show-error --connect-timeout 30 --max-time 300 "$agy_url" --output "$agy_archive"; \
    printf '%%s  %%s\n' "$agy_digest" "$agy_archive" | sha512sum -c -; \
    test "$(tar -tzf "$agy_archive")" = antigravity; \
    test "$(tar -tvzf "$agy_archive" | cut -c1)" = -; \
    mkdir -p /opt/cooper/bin /opt/cooper/libexec; \
    tar -xOzf "$agy_archive" antigravity > /opt/cooper/libexec/agy; \
    chmod 0755 /opt/cooper/libexec/agy; \
    test "$(/opt/cooper/libexec/agy --version)" = '%s'; \
    rm "$agy_archive"

# Both execution modes use file state, even if a workload starts its own bus.
RUN %s

# Native 1.2.2 expects Playwright 1.57.0. Its old driver CDN returns 404.
# Use the same official driver via npm and the image's Linux Node runtime.
# These executables are image-owned; a shared macOS cache must not hide them.
RUN npm install --prefix /opt/cooper/agy-playwright --ignore-scripts --no-audit --no-fund playwright@1.57.0 \
    && ln -s node_modules/playwright /opt/cooper/agy-playwright/package \
    && test "$(node /opt/cooper/agy-playwright/package/cli.js --version)" = 'Version 1.57.0'
`, amd.URL, amd.SHA512, arm.URL, arm.SHA512, version,
		antigravity.FileWrapperCommand("/opt/cooper/libexec/agy", "/opt/cooper/bin/agy")), nil
}
