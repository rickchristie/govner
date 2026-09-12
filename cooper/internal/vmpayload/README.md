# Embedded virtiofsd

Cooper includes a compressed, static `virtiofsd` 1.14.0 executable for Linux
x86-64. The VM supervisor runs it as the invoking user with no Linux
capabilities. Version 1.14.0 supports the soft UID and GID translation that
this unprivileged mode needs. Version 1.10.0 always tried to restore UID and
GID 0 after a request. That operation failed in Cooper's capability-free
supervisor and made nested file-system behavior unreliable.

The executable comes from upstream commit
`c2540f8db14caba81c1e37fba23fc7bf2cd7f0dd`. The source archive SHA-256 is
`52b66e449ca583b4f050a2bff327ff812211a2c349b4130279fcfc6a64540f04`.
The uncompressed executable SHA-256 is
`3bde9d848edf61fd30448dbd98c016533500ecffbf990455a0bd72e34b7a26b3`.

Run `./cooper/dev/build-virtiofsd.sh` from the repository root to reproduce
the compressed payload. The script pins the Rust builder image, upstream
source hash, Cargo lock file, Alpine build packages, and final executable
hash. Cooper also verifies the size and hash before it writes the helper to a
Docker build context.

Upstream source: <https://gitlab.com/virtio-fs/virtiofsd/-/tree/v1.14.0>

`virtiofsd` is available under Apache-2.0 and BSD-3-Clause terms. See the
license files in this directory.
