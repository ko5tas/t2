#!/bin/bash
set -euo pipefail

BINARY="$1"
VERSION="$2"
ARCH="$3"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PKG_NAME="t2_${VERSION}_${ARCH}"
BUILD_DIR="$(mktemp -d)"
trap "rm -rf ${BUILD_DIR}" EXIT

# Create directory structure
mkdir -p "${BUILD_DIR}/DEBIAN"
mkdir -p "${BUILD_DIR}/usr/bin"
mkdir -p "${BUILD_DIR}/etc/t2"
mkdir -p "${BUILD_DIR}/lib/systemd/system"

# Copy binary
cp "${BINARY}" "${BUILD_DIR}/usr/bin/t2"
chmod 0755 "${BUILD_DIR}/usr/bin/t2"

# Copy config (placeholder)
cp "${SCRIPT_DIR}/../config.example.yaml" "${BUILD_DIR}/etc/t2/config.yaml"
chmod 0640 "${BUILD_DIR}/etc/t2/config.yaml"

# Copy systemd unit
cp "${SCRIPT_DIR}/t2.service" "${BUILD_DIR}/lib/systemd/system/t2.service"
chmod 0644 "${BUILD_DIR}/lib/systemd/system/t2.service"

# Copy maintainer scripts
for script in postinst prerm postrm; do
    cp "${SCRIPT_DIR}/${script}" "${BUILD_DIR}/DEBIAN/${script}"
    chmod 0755 "${BUILD_DIR}/DEBIAN/${script}"
done

# Copy conffiles
cp "${SCRIPT_DIR}/conffiles" "${BUILD_DIR}/DEBIAN/conffiles"

# Generate control file with substitutions
SIZE_KB=$(du -sk "${BUILD_DIR}" | cut -f1)
sed -e "s/\${VERSION}/${VERSION}/" \
    -e "s/\${ARCH}/${ARCH}/" \
    -e "s/\${SIZE_KB}/${SIZE_KB}/" \
    "${SCRIPT_DIR}/control.tmpl" > "${BUILD_DIR}/DEBIAN/control"

# Build deb
dpkg-deb --build "${BUILD_DIR}" "${PKG_NAME}.deb"
echo "Built: ${PKG_NAME}.deb"
