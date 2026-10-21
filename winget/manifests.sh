#!/bin/sh
# Write winget manifests for a release.
# Usage: winget/manifests.sh VERSION AMD64_EXE ARM64_EXE OUTDIR
# The exe paths are only hashed; the manifests point at the GitHub release.
set -eu
version=$1 amd64=$2 arm64=$3 out=$4
id=daniel-widrick.ssh-copy-id
repo=https://github.com/daniel-widrick/ssh-copy-id
schema=1.10.0
sha() { sha256sum < "$1" | cut -d' ' -f1 | tr a-f A-F; }

mkdir -p "$out"
cat > "$out/$id.yaml" <<EOF
# yaml-language-server: \$schema=https://aka.ms/winget-manifest.version.$schema.schema.json
PackageIdentifier: $id
PackageVersion: $version
DefaultLocale: en-US
ManifestType: version
ManifestVersion: $schema
EOF

cat > "$out/$id.installer.yaml" <<EOF
# yaml-language-server: \$schema=https://aka.ms/winget-manifest.installer.$schema.schema.json
PackageIdentifier: $id
PackageVersion: $version
InstallerType: portable
Commands:
- ssh-copy-id
Installers:
- Architecture: x64
  InstallerUrl: $repo/releases/download/v$version/ssh-copy-id_windows_amd64.exe
  InstallerSha256: $(sha "$amd64")
- Architecture: arm64
  InstallerUrl: $repo/releases/download/v$version/ssh-copy-id_windows_arm64.exe
  InstallerSha256: $(sha "$arm64")
ManifestType: installer
ManifestVersion: $schema
EOF

cat > "$out/$id.locale.en-US.yaml" <<EOF
# yaml-language-server: \$schema=https://aka.ms/winget-manifest.defaultLocale.$schema.schema.json
PackageIdentifier: $id
PackageVersion: $version
PackageLocale: en-US
Publisher: Daniel Widrick
PublisherUrl: https://github.com/daniel-widrick
PackageName: ssh-copy-id
PackageUrl: $repo
License: MIT
LicenseUrl: $repo/blob/main/LICENSE
ShortDescription: Install your SSH public keys on a remote host's authorized_keys, natively on Windows.
Description: A Go port of OpenSSH's ssh-copy-id that drives the Windows OpenSSH client, so ssh_config, the agent, known_hosts and password prompts work as they do for plain ssh.
Tags:
- ssh
- openssh
- authorized-keys
ReleaseNotesUrl: $repo/releases/tag/v$version
ManifestType: defaultLocale
ManifestVersion: $schema
EOF
