package templates

import (
	"fmt"

	"github.com/rickchristie/govner/cooper/internal/antigravity"
	"github.com/rickchristie/govner/cooper/internal/config"
)

func renderAntigravityInstall(cfg *config.Config, version string) (string, error) {
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

# Read the driver required by this exact native executable. Old native
# releases use a driver CDN that returns 404, so install it through npm.
# Releases without this dependency do not need a Playwright driver.
# These executables are image-owned; a shared macOS cache must not hide them.
RUN %s
RUN set -eu; \
    agy_driver_version=$(/opt/cooper/libexec/agy-driver-version /opt/cooper/libexec/agy); \
    if [ "$agy_driver_version" != none ]; then \
      npm install --prefix /opt/cooper/agy-playwright --ignore-scripts --no-audit --no-fund "playwright@$agy_driver_version"; \
      ln -s node_modules/playwright /opt/cooper/agy-playwright/package; \
      test "$(node /opt/cooper/agy-playwright/package/cli.js --version)" = "Version $agy_driver_version"; \
    fi
`, amd.URL, amd.SHA512, arm.URL, arm.SHA512, version,
		antigravity.FileWrapperCommand("/opt/cooper/libexec/agy", "/opt/cooper/bin/agy"),
		antigravity.DriverVersionCommand("/opt/cooper/libexec/agy-driver-version")), nil
}
