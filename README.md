# ssh-copy-id for Windows

A Go port of OpenSSH's `ssh-copy-id` that runs natively on Windows. It drives
the system `ssh` (the Windows OpenSSH client), so `~/.ssh/config`, the agent,
`known_hosts` and password prompts all behave as they do for plain `ssh`.

Manual install: download `ssh-copy-id_windows_amd64.exe` (or `_arm64`) from
the [latest release](https://github.com/daniel-widrick/ssh-copy-id/releases/latest),
then from an elevated PowerShell:

```powershell
Copy-Item "$HOME\Downloads\ssh-copy-id_windows_amd64.exe" "$env:WINDIR\System32\ssh-copy-id.exe"
```

```
winget install daniel-widrick.ssh-copy-id
ssh-copy-id user@host
ssh-copy-id -i ~/.ssh/id_ed25519 -p 2222 user@host
```

## Usage

```
ssh-copy-id [-f] [-n] [-x] [-i [identity_file]] [-t target_path] [-F ssh_config] [[-o ssh_option] ...] [-p port] [user@]hostname
```

| flag | meaning |
|------|---------|
| `-i file` | public key to install (`.pub` is appended if missing) |
| `-f` | append even if the key is already present |
| `-n` | dry run: report what would be added, change nothing |
| `-t path` | remote authorized_keys path (default `.ssh/authorized_keys`) |
| `-p`, `-o`, `-F` | passed through to `ssh` |
| `-x` | print the ssh command being run |

With no `-i`, keys come from the agent (`ssh-add -L`), and failing that the
newest `~/.ssh/id*.pub`, the same as the OpenSSH script.

## How it works

One ssh connection (so at most one password prompt). Keys go over stdin to a
small POSIX `sh` script on the remote that creates `~/.ssh` with umask 077,
repairs a missing trailing newline, skips any key whose `type base64` pair is
already in the file (whatever its options or comment), appends the rest and
runs `restorecon` when SELinux tools are present. CRLF line endings from
Windows-edited key files are stripped first.

The remote must have a POSIX `sh` (any Linux, BSD or macOS host). Windows
OpenSSH servers are not supported.

## Releasing

Push a `v*` tag. `.github/workflows/release.yml` tests, builds
`ssh-copy-id_windows_{amd64,arm64}.exe`, and attaches them, `SHA256SUMS` and
`winget-manifests.zip` to a GitHub release.

The first winget submission is done once by hand: unzip the manifests into
`manifests/d/daniel-widrick/ssh-copy-id/<version>/` in a fork of
[microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs) and open a
PR (or run `wingetcreate submit <dir>`). After it merges, add a `WINGET_TOKEN`
repo secret (classic PAT, `public_repo` scope) and later tags open the
winget-pkgs update PR automatically.

Check manifests locally with `winget validate --manifest <dir>`.
