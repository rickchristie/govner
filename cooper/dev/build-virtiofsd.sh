#!/usr/bin/env bash

set -Eeuo pipefail

readonly VIRTIOFSD_VERSION=1.14.0
readonly VIRTIOFSD_COMMIT=c2540f8db14caba81c1e37fba23fc7bf2cd7f0dd
readonly SOURCE_SHA256=52b66e449ca583b4f050a2bff327ff812211a2c349b4130279fcfc6a64540f04
readonly BINARY_SHA256=3bde9d848edf61fd30448dbd98c016533500ecffbf990455a0bd72e34b7a26b3
readonly RUST_IMAGE=rust@sha256:64eba3726734dcfe89e0a62a0485007a3ab7c7372ce5b38c621d8812f70215f0

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cooper_dir="$(cd -- "$script_dir/.." && pwd)"
output="$cooper_dir/internal/vmpayload/virtiofsd-v${VIRTIOFSD_VERSION}-linux-amd64.gz"
work_dir="$(mktemp -d -t cooper-virtiofsd-build.XXXXXX)"
image="cooper-virtiofsd-builder:${VIRTIOFSD_VERSION}"
container="cooper-virtiofsd-extract-$$"

cleanup() {
    docker rm -f "$container" >/dev/null 2>&1 || true
    rm -rf -- "$work_dir"
}
trap cleanup EXIT

curl --fail --location --retry 5 --retry-all-errors \
    --output "$work_dir/virtiofsd.tar.gz" \
    "https://gitlab.com/virtio-fs/virtiofsd/-/archive/v${VIRTIOFSD_VERSION}/virtiofsd-v${VIRTIOFSD_VERSION}.tar.gz"
printf '%s  %s\n' "$SOURCE_SHA256" "$work_dir/virtiofsd.tar.gz" | sha256sum --check --strict

cat >"$work_dir/Dockerfile" <<EOF
FROM $RUST_IMAGE AS build
RUN apk add --no-cache \\
      libcap-ng-static=0.8.5-r0 \\
      libseccomp-static=2.6.0-r0 \\
      musl=1.2.5-r12 \\
      musl-dev=1.2.5-r12
COPY virtiofsd.tar.gz /tmp/virtiofsd.tar.gz
RUN mkdir /src \\
    && tar -xzf /tmp/virtiofsd.tar.gz -C /src --strip-components=1 \\
    && test "\$(sed -n 's/^version = "\([^"]*\)"/\1/p' /src/Cargo.toml | head -n 1)" = "$VIRTIOFSD_VERSION" \\
    && cd /src \\
    && RUSTFLAGS='-C target-feature=+crt-static -C link-self-contained=yes' \\
       LIBSECCOMP_LINK_TYPE=static \\
       LIBSECCOMP_LIB_PATH=/usr/lib \\
       LIBCAPNG_LINK_TYPE=static \\
       LIBCAPNG_LIB_PATH=/usr/lib \\
       cargo build --locked --release --target x86_64-unknown-linux-musl \\
    && strip /src/target/x86_64-unknown-linux-musl/release/virtiofsd

FROM scratch
COPY --from=build /src/target/x86_64-unknown-linux-musl/release/virtiofsd /virtiofsd
EOF

docker build --pull=false --tag "$image" "$work_dir"
docker create --name "$container" "$image" /virtiofsd --version >/dev/null
docker cp "$container:/virtiofsd" "$work_dir/virtiofsd" >/dev/null
printf '%s  %s\n' "$BINARY_SHA256" "$work_dir/virtiofsd" | sha256sum --check --strict
test "$("$work_dir/virtiofsd" --version)" = "virtiofsd $VIRTIOFSD_VERSION"

mkdir -p -- "$(dirname -- "$output")"
gzip --no-name --best --stdout "$work_dir/virtiofsd" >"$output.new"
mv -- "$output.new" "$output"
chmod 0644 "$output"

printf 'Wrote %s from virtiofsd commit %s.\n' "$output" "$VIRTIOFSD_COMMIT"
