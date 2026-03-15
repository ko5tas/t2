# Debian Packaging & Release Pipeline Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `.deb` packages for arm64/armhf on GitHub release, serve via GPG-signed apt repo on GitHub Pages, auto-restart service on upgrade.

**Architecture:** Packaging assets live in `packaging/`. A shell script `build-deb.sh` assembles the deb from a pre-compiled binary. GitHub Actions workflow cross-compiles, builds debs, uploads as release assets, and updates the apt repo on `gh-pages`.

**Tech Stack:** Go cross-compilation, dpkg-deb, dpkg-scanpackages, apt-ftparchive, GPG, GitHub Actions, GitHub Pages

**Spec:** `docs/superpowers/specs/2026-03-15-deb-packaging-design.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `packaging/t2.service` | systemd unit file |
| `packaging/postinst` | Creates t2 user, restarts service |
| `packaging/prerm` | Stops service on removal |
| `packaging/postrm` | Cleans up user/config on purge |
| `packaging/conffiles` | Lists conffiles for dpkg |
| `packaging/control.tmpl` | DEBIAN/control template with placeholders |
| `packaging/build-deb.sh` | Assembles deb directory and runs dpkg-deb |
| `.github/workflows/release.yml` | CI: build, package, sign, publish apt repo |
| `README.md` | Add installation and service management docs |

---

## Chunk 1: Packaging Assets

### Task 1: Create systemd unit file

**Files:**
- Create: `packaging/t2.service`

- [ ] **Step 1: Write the systemd unit**

```ini
[Unit]
Description=T2 Trading212 Dashboard
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/bin/t2
Restart=on-failure
RestartSec=5
User=t2
Group=t2

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 2: Commit**

```bash
git add packaging/t2.service
git commit -m "feat: add systemd unit file for t2 service"
```

### Task 2: Create maintainer scripts

**Files:**
- Create: `packaging/postinst`
- Create: `packaging/prerm`
- Create: `packaging/postrm`
- Create: `packaging/conffiles`

- [ ] **Step 1: Write postinst**

```bash
#!/bin/sh
set -e

# Create system user on first install
if ! getent passwd t2 >/dev/null; then
    adduser --system --group --no-create-home --shell /usr/sbin/nologin t2
fi

# Ensure config directory permissions
chown root:t2 /etc/t2/config.yaml
chmod 0640 /etc/t2/config.yaml

systemctl daemon-reload
systemctl enable t2
systemctl restart t2
```

- [ ] **Step 2: Write prerm**

```bash
#!/bin/sh
set -e
if [ "$1" = "remove" ]; then
    systemctl stop t2
    systemctl disable t2
fi
```

- [ ] **Step 3: Write postrm**

```bash
#!/bin/sh
set -e
if [ "$1" = "purge" ]; then
    deluser --system t2 || true
    rm -rf /etc/t2
fi
```

- [ ] **Step 4: Write conffiles**

```
/etc/t2/config.yaml
```

- [ ] **Step 5: Make scripts executable and commit**

```bash
chmod +x packaging/postinst packaging/prerm packaging/postrm
git add packaging/postinst packaging/prerm packaging/postrm packaging/conffiles
git commit -m "feat: add deb maintainer scripts (postinst, prerm, postrm, conffiles)"
```

### Task 3: Create control template

**Files:**
- Create: `packaging/control.tmpl`

- [ ] **Step 1: Write control template**

The `${VERSION}`, `${ARCH}`, and `${SIZE_KB}` placeholders are replaced by `build-deb.sh` at build time.

```
Package: t2
Version: ${VERSION}
Architecture: ${ARCH}
Installed-Size: ${SIZE_KB}
Maintainer: ko5tas <ko5tas@users.noreply.github.com>
Homepage: https://github.com/ko5tas/t2
Description: Trading212 portfolio dashboard
 Web dashboard for viewing Trading212 portfolio positions.
Depends: systemd
Recommends: ca-certificates
Section: misc
Priority: optional
```

- [ ] **Step 2: Commit**

```bash
git add packaging/control.tmpl
git commit -m "feat: add DEBIAN/control template"
```

### Task 4: Create build-deb.sh

**Files:**
- Create: `packaging/build-deb.sh`

- [ ] **Step 1: Write the build script**

This script takes three arguments: `BINARY_PATH`, `VERSION`, `ARCH` (arm64 or armhf).

```bash
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
```

- [ ] **Step 2: Make executable and commit**

```bash
chmod +x packaging/build-deb.sh
git add packaging/build-deb.sh
git commit -m "feat: add build-deb.sh script for assembling deb packages"
```

- [ ] **Step 3: Test locally (dry run)**

Run the script locally to verify it produces a valid deb structure (won't produce a working ARM binary on macOS, but validates the packaging logic):

```bash
# Create a dummy binary for testing
echo "dummy" > /tmp/t2-dummy
chmod +x /tmp/t2-dummy
bash packaging/build-deb.sh /tmp/t2-dummy 0.0.0 arm64
# Verify the deb was created
ls -la t2_0.0.0_arm64.deb
# Clean up
rm -f t2_0.0.0_arm64.deb /tmp/t2-dummy
```

Expected: `t2_0.0.0_arm64.deb` file is created without errors. (If `dpkg-deb` is not available on macOS, this step can be skipped — CI will validate it.)

---

## Chunk 2: GitHub Actions Workflow

### Task 5: Create release workflow

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Write the workflow**

```yaml
name: Release

on:
  release:
    types: [published]

concurrency: release-apt

permissions:
  contents: write
  pages: write

jobs:
  build:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        include:
          - goos: linux
            goarch: arm64
            deb_arch: arm64
          - goos: linux
            goarch: arm
            goarm: "7"
            deb_arch: armhf
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - name: Build binary
        env:
          GOOS: ${{ matrix.goos }}
          GOARCH: ${{ matrix.goarch }}
          GOARM: ${{ matrix.goarm }}
          CGO_ENABLED: "0"
        run: go build -o t2-${{ matrix.deb_arch }} ./cmd/t2

      - name: Extract version
        id: version
        run: |
          TAG="${{ github.event.release.tag_name }}"
          VERSION="${TAG#v}"
          echo "version=${VERSION}" >> "$GITHUB_OUTPUT"

      - name: Build deb package
        run: |
          bash packaging/build-deb.sh \
            t2-${{ matrix.deb_arch }} \
            ${{ steps.version.outputs.version }} \
            ${{ matrix.deb_arch }}

      - name: Upload deb to release
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          gh release upload "${{ github.event.release.tag_name }}" \
            t2_${{ steps.version.outputs.version }}_${{ matrix.deb_arch }}.deb

      - uses: actions/upload-artifact@v4
        with:
          name: deb-${{ matrix.deb_arch }}
          path: "*.deb"

  publish-repo:
    needs: build
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@v4
        with:
          path: debs
          merge-multiple: true

      - name: Checkout gh-pages
        uses: actions/checkout@v4
        with:
          ref: gh-pages
          path: repo
          fetch-depth: 0

      - name: Copy debs to pool
        run: |
          mkdir -p repo/pool/main
          cp debs/*.deb repo/pool/main/

      - name: Generate package indices
        run: |
          cd repo
          mkdir -p dists/stable/main/binary-arm64
          mkdir -p dists/stable/main/binary-armhf
          dpkg-scanpackages --arch arm64 pool/ > dists/stable/main/binary-arm64/Packages
          dpkg-scanpackages --arch armhf pool/ > dists/stable/main/binary-armhf/Packages
          gzip -kf dists/stable/main/binary-arm64/Packages
          gzip -kf dists/stable/main/binary-armhf/Packages

      - name: Generate Release file
        run: |
          cd repo
          apt-ftparchive release dists/stable/ > dists/stable/Release

      - name: Import GPG key and sign
        env:
          GPG_PRIVATE_KEY: ${{ secrets.GPG_PRIVATE_KEY }}
        run: |
          echo "${GPG_PRIVATE_KEY}" | gpg --batch --import
          cd repo
          gpg --batch --yes --local-user "t2 APT Repository" --detach-sign -o dists/stable/Release.gpg dists/stable/Release
          gpg --batch --yes --local-user "t2 APT Repository" --clearsign -o dists/stable/InRelease dists/stable/Release

      - name: Commit and push
        run: |
          cd repo
          git config user.name "github-actions[bot]"
          git config user.email "github-actions[bot]@users.noreply.github.com"
          git add -A
          git commit -m "Update apt repo for ${{ github.event.release.tag_name }}" || true
          git push
```

- [ ] **Step 2: Commit**

```bash
mkdir -p .github/workflows
git add .github/workflows/release.yml
git commit -m "feat: add GitHub Actions release workflow for deb packages"
```

### Task 6: Initialize gh-pages branch with GPG public key

This task requires the user to have already generated the GPG key and added the private key as a GitHub secret.

- [ ] **Step 1: Create orphan gh-pages branch**

```bash
git checkout --orphan gh-pages
git rm -rf .
```

- [ ] **Step 2: Add GPG public key and .nojekyll**

Export the public key (user must have generated it already):
```bash
gpg --armor --export "t2 APT Repository" > t2-repo.gpg
touch .nojekyll
```

The directory structure (`dists/`, `pool/`) will be created by the first release workflow run.

- [ ] **Step 3: Commit and push**

```bash
git add t2-repo.gpg .nojekyll
git commit -m "Initialize apt repo with GPG public key"
git push -u origin gh-pages
```

- [ ] **Step 4: Enable GitHub Pages**

Go to GitHub repo Settings > Pages > Source: Deploy from branch `gh-pages`, root `/`.

- [ ] **Step 5: Switch back to working branch**

```bash
git checkout claude/practical-sanderson
```

---

## Chunk 3: Documentation

### Task 7: Update README with installation docs

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add Installation section after Quick Start**

Add the following sections to `README.md` after the existing "Quick Start" section:

```markdown
## Installation (Debian/DietPi)

### Add the apt repository

```bash
# Import GPG key
curl -fsSL https://ko5tas.github.io/t2/t2-repo.gpg | sudo gpg --dearmor -o /usr/share/keyrings/t2-repo.gpg

# Add repository
echo "deb [signed-by=/usr/share/keyrings/t2-repo.gpg] https://ko5tas.github.io/t2 stable main" | sudo tee /etc/apt/sources.list.d/t2.list

# Install
sudo apt update && sudo apt install t2
```

### Configure

Edit `/etc/t2/config.yaml` with your Trading212 API credentials, then restart:

```bash
sudo systemctl restart t2
```

### Upgrade

```bash
sudo apt update && sudo apt upgrade
```

The service restarts automatically after upgrade.

### Service management

```bash
sudo systemctl status t2    # Check status
sudo systemctl restart t2   # Restart
sudo systemctl stop t2      # Stop
sudo journalctl -u t2 -f    # View logs
```
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add apt installation and service management to README"
```

### Task 8: Final commit and PR

- [ ] **Step 1: Push and create PR**

```bash
git push
gh pr create --title "Add Debian packaging and release pipeline" --body "$(cat <<'EOF'
## Summary
- Add packaging assets (systemd unit, maintainer scripts, control template, build-deb.sh)
- Add GitHub Actions workflow triggered on release: cross-compile arm64/armhf, build .deb, sign apt repo, publish to GitHub Pages
- Update README with apt installation and service management docs

## Setup required
1. Generate GPG key and add `GPG_PRIVATE_KEY` secret to GitHub
2. Initialize `gh-pages` branch with public key
3. Enable GitHub Pages on the repo

## Test plan
- [ ] Create a test release and verify workflow completes
- [ ] Verify .deb files appear as release assets
- [ ] Verify apt repo is accessible at https://ko5tas.github.io/t2
- [ ] Install on DietPi via apt and verify service starts

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```
