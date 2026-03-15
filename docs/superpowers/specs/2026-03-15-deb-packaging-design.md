# Debian Packaging & Release Pipeline

## Goal

Automate building `.deb` packages for arm64 and armhf on every GitHub release, serve them via a GPG-signed apt repository on GitHub Pages, and auto-restart the t2 service on upgrades.

## Deb Package Structure

Package name: `t2`
Architectures: `arm64`, `armhf`
Version: derived from release tag, stripped of any `v` prefix (tag `0.1.0` → version `0.1.0`)

### Installed files

| Path | Purpose | Permissions |
|------|---------|-------------|
| `/usr/bin/t2` | Binary | 0755, root:root |
| `/etc/t2/config.yaml` | Config with placeholder credentials | 0640, root:t2 |
| `/lib/systemd/system/t2.service` | systemd unit | 0644, root:root |

### DEBIAN/conffiles

```
/etc/t2/config.yaml
```

This ensures apt preserves user edits on upgrade.

### Maintainer scripts

**postinst** (runs after install/upgrade):
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

**prerm** (runs before removal, not upgrade):
```bash
#!/bin/sh
set -e
if [ "$1" = "remove" ]; then
    systemctl stop t2
    systemctl disable t2
fi
```

**postrm** (runs after removal, cleans up on purge):
```bash
#!/bin/sh
set -e
if [ "$1" = "purge" ]; then
    deluser --system t2 || true
    rm -rf /etc/t2
fi
```

### systemd unit

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

### DEBIAN/control

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

## GitHub Actions Pipeline

Single workflow: `.github/workflows/release.yml`
Trigger: `on: release: types: [published]`
Concurrency: `concurrency: release-apt` (prevents parallel releases from corrupting gh-pages)

### Steps

1. **Checkout** source code
2. **Set up Go** (match go.mod version)
3. **Build matrix** (parallel):
   - `GOOS=linux GOARCH=arm64` → `t2_${VERSION}_arm64.deb`
   - `GOOS=linux GOARCH=arm GOARM=7` → `t2_${VERSION}_armhf.deb`
4. **Assemble deb** for each arch:
   - Create directory structure: `DEBIAN/`, `usr/bin/`, `etc/t2/`, `lib/systemd/system/`
   - Copy binary, config, unit file, control, conffiles, postinst, prerm, postrm
   - Compute `Installed-Size` from binary size
   - `dpkg-deb --build`
5. **Upload debs** as release assets via `gh release upload`
6. **Update apt repo** on `gh-pages`:
   - Checkout `gh-pages` branch
   - Copy new `.deb` files into `pool/main/`
   - Generate per-arch package indices:
     ```bash
     dpkg-scanpackages --arch arm64 pool/ > dists/stable/main/binary-arm64/Packages
     dpkg-scanpackages --arch armhf pool/ > dists/stable/main/binary-armhf/Packages
     ```
   - Compress: `gzip -k` each `Packages` file
   - Generate `Release` with `apt-ftparchive release dists/stable/`
   - Import GPG key from `GPG_PRIVATE_KEY` secret
   - Sign: `gpg --detach-sign -o Release.gpg Release` and `gpg --clearsign -o InRelease Release`
   - Commit and push to `gh-pages`

### Repo directory layout on gh-pages

```
/
├── t2-repo.gpg              # Public GPG key (armored)
├── dists/
│   └── stable/
│       ├── main/
│       │   ├── binary-arm64/
│       │   │   ├── Packages
│       │   │   └── Packages.gz
│       │   └── binary-armhf/
│       │       ├── Packages
│       │       └── Packages.gz
│       ├── Release
│       ├── Release.gpg
│       └── InRelease
└── pool/
    └── main/
        ├── t2_0.1.0_arm64.deb
        └── t2_0.1.0_armhf.deb
```

## GPG Signing

### One-time setup

Generate key locally:
```bash
gpg --batch --gen-key <<EOF
%no-protection
Key-Type: RSA
Key-Length: 4096
Name-Real: t2 APT Repository
Name-Email: ko5tas@users.noreply.github.com
Expire-Date: 0
EOF
```

Add to GitHub:
- `GPG_PRIVATE_KEY` secret: output of `gpg --armor --export-secret-keys "t2 APT Repository"`

Export public key for the repo:
```bash
gpg --armor --export "t2 APT Repository" > t2-repo.gpg
```
This gets committed to `gh-pages` root.

## DietPi Setup

One-time commands:
```bash
curl -fsSL https://ko5tas.github.io/t2/t2-repo.gpg | sudo gpg --dearmor -o /usr/share/keyrings/t2-repo.gpg

echo "deb [signed-by=/usr/share/keyrings/t2-repo.gpg] https://ko5tas.github.io/t2 stable main" | sudo tee /etc/apt/sources.list.d/t2.list

sudo apt update && sudo apt install t2
```

Then edit `/etc/t2/config.yaml` with API credentials. Future upgrades: `sudo apt update && sudo apt upgrade`.

## Files to Create

| File | Purpose |
|------|---------|
| `.github/workflows/release.yml` | CI pipeline |
| `packaging/t2.service` | systemd unit |
| `packaging/postinst` | Post-install script |
| `packaging/prerm` | Pre-remove script |
| `packaging/postrm` | Post-remove script (purge cleanup) |
| `packaging/conffiles` | Lists config files apt should preserve |
| `packaging/control.tmpl` | DEBIAN/control template |
| `packaging/build-deb.sh` | Script to assemble deb from binary |

## README Updates

Add sections for:
- Installation via apt (DietPi/Debian/Ubuntu ARM)
- DietPi setup commands (GPG key, repo, install)
- systemd service management (`systemctl status/restart/stop t2`)
- Upgrading (`apt update && apt upgrade`)
