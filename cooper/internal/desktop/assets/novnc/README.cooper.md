# noVNC source

These files come from noVNC 1.6.0, Debian package `1:1.6.0-2`. The Debian
package was obtained through its signed APT repository. `SHA256SUMS` records
the exact imported files. Source: <https://github.com/novnc/noVNC>.

Cooper includes `core/` and its pako dependency without content changes.
The upstream `vendor/pako/` directory is stored as `dependencies/pako/` because
Go removes `vendor` directories from module archives. The asset handler maps
upstream `vendor/` URLs to these files. This keeps both `go install` and local
builds complete without changing upstream imports. `SHA256SUMS` uses the
stored paths; the license notices retain their upstream paths.
`COPYRIGHT` contains notices and license terms. `LICENSE-MPL-2.0` contains
the main license. The viewer serves these files from the host binary.

Do not load viewer code from a workload or a CDN. A guest can write its own
filesystem. It must not be able to change code that runs in the host browser.
To update, import the same upstream directories, keep this path mapping,
preserve all notices, update this version and the file hashes, and run the
desktop asset, browser, and VM checks. Check the downloaded Go module too.
