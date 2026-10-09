# Oberon 

[!(https://github.com/oberon-core/oberon/actions/workflows/build.yml) [![Version](https://img.shields.io/badge/version-0.4.3-6f42c1)](https://github.com/oberon-core/oberon) [![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go)](https://go.dev/) [![License](https://img.shields.io/github/license/oberon-core/oberon)](LICENSE)

**An AmneziaWG WARP client for profile management, endpoint discovery, and private local proxies.**

[🌐 English](README.md) · [فارسی](README.fa.md)

🌐 Oberon creates and reuses WARP profiles, checks which endpoints complete an AmneziaWG handshake, and connects through local SOCKS5/HTTP proxies or a system tunnel.

## ✨ Highlights

- 🪄 **One-time first-run setup:** the TUI registers and saves a profile only when no valid profile is found. Later connections reuse its key.
- 🔎 **Endpoint scan on every connection:** Oberon tests candidates, shows live progress, and selects the fastest successful handshake.
- 🔌 **Two connection modes:** local SOCKS5 and HTTP proxies, or a full-system tunnel on Linux and Windows.
- 📱 **Android and Termux:** an Android ARM64 command-line build and a Termux bundle; Android supports proxy mode, not a system-wide tunnel. The Android build is not an APK.
- 🔑 **Profile control:** browse working endpoints, or create a fresh WARP key and apply it to a saved profile with a backup.
- 🎨 **Full-screen terminal UI:** English menus, consistent pages, colored scan progress, and animated registration and connection indicators.

## 🚀 Quick start

### 🛠️ Build from source

Oberon requires **Go 1.25.0 or newer**.

```sh
git clone https://github.com/oberon-core/oberon.git
cd oberon
go build -trimpath -o oberon .
```

Start the interactive menu:

```sh
./oberon
```

On Windows PowerShell:

```powershell
go build -trimpath -o .\oberon.exe .
.\oberon.exe
```

## 📱 Android and Termux

Releases include `oberon-<tag>-android-arm64.tar.gz`, an Android ARM64 command-line build, and `oberon-<tag>-termux-aarch64.tar.gz`, a Termux bundle with an installer. The Android build is a command-line executable, not an APK.

In Termux, extract the bundle and install Oberon into Termux's `bin` directory:
Replace `<tag>` with the version tag in the downloaded archive name.

```sh
tar -xzf oberon-<tag>-termux-aarch64.tar.gz
bash ./install-termux.sh
oberon --version
```

Android currently supports Oberon's local proxy mode. Create a profile and connect through SOCKS5 and HTTP:

```sh
oberon warp create -out warp-awg.conf
oberon connect -config warp-awg.conf -socks 127.0.0.1:1717 -http 127.0.0.1:1718
```

The Android build does not configure the device's system routes or create a system TUN interface.

## ▶️ Connect with the TUI

Run Oberon without arguments and choose **Connect Oberon**:

1. Oberon looks for a valid `.conf` profile beside the executable or in the current directory.
2. If none exists, Oberon registers a WARP account once and saves a profile. It tries the executable directory first and then the current directory when that is a different location.
3. Before every connection, Oberon scans endpoints and chooses the fastest successful handshake.
4. Oberon starts both local proxies.

The connection screen shows the selected endpoint and proxy details:

| Proxy | Default address |
| --- | --- |
| SOCKS5 | `127.0.0.1:1717` |
| HTTP | `http://127.0.0.1:1718` |

After connecting, the screen shows the endpoint and both proxy addresses, then ends with **Oberon Connected**.

The saved WARP key is reused on later connections. Choose **Create WARP Key & Apply** to rotate the key; Oberon keeps a `.bak` copy of the previous profile.

**Scan Endpoint & Manual Connection** lists working endpoints so you can choose one with the arrow keys and connect through it. If arrow-key navigation is unavailable, select an endpoint by number.

## 💻 CLI reference

| Command | What it does |
| --- | --- |
| `oberon` or `oberon tui` | Open the interactive terminal menu. |
| `oberon create -out warp-awg.conf` | Register a new WARP profile, scan endpoints, and save the result. |
| `oberon scan -config warp-awg.conf -mode fast` | Scan a saved profile and list working endpoints. |
| `oberon scan -config warp-awg.conf -P -o selected.conf` | Save a copy of the profile using the fastest successful endpoint. |
| `oberon connect -config warp-awg.conf` | Scan and start a full-system tunnel. |
| `oberon connect -config warp-awg.conf -socks 127.0.0.1:1717 -http 127.0.0.1:1718` | Scan and start local proxies through an in-memory tunnel. |
| `oberon --version` | Print the Oberon version and build target. |

`oberon warp create` is an alias for `oberon create`. `oberon run profile.conf` remains available as a legacy connection command.

### 🔌 Proxy-only connection

Use one or both proxy flags:

```sh
oberon connect -config warp-awg.conf -socks 127.0.0.1:1717
oberon connect -config warp-awg.conf -http 127.0.0.1:1718
oberon connect -config warp-awg.conf -socks 127.0.0.1:1717 -http 127.0.0.1:1718
```

Proxy mode uses an in-memory tunnel. It does not change system routes, require Administrator or root privileges, or use Wintun.

### 🔎 Scan modes

- `-mode fast` checks Oberon’s curated endpoint pool.
- `-mode all` checks a much wider address and port set and can take significantly longer.
- `-workers` controls parallel checks; `-timeout` sets the handshake timeout per endpoint.

```sh
oberon connect -config warp-awg.conf -mode all -workers 24 -timeout 1s
```

A successful handshake measures endpoint reachability and latency; it is not a throughput benchmark.

## 🛡️ Full-system tunnel requirements

Full-system mode changes network routes and needs elevated permissions:

- **Linux:** run as root, for example `sudo ./oberon connect -config warp-awg.conf`; the `ip` command from iproute2 must be available.
- **Windows:** Oberon requests approval through UAC automatically. The matching **Wintun 0.14.1** DLL must be next to `oberon.exe`.

From the project directory, the setup script downloads the Wintun archive, verifies its SHA-256 hash, and installs the DLL:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\setup-wintun.ps1 -Architecture amd64
```

Use `arm64`, `x86`, or `arm` for the matching Windows build. If `oberon.exe` is in a different directory from the project, place `wintun.dll` beside the executable.

The build matrix includes Linux, Windows, macOS, FreeBSD, and OpenBSD targets. Full-system route setup is currently implemented for Linux and Windows.

## 🧪 Build and test

```sh
go build -trimpath -o oberon .
go test ./...
bash ./scripts/build-matrix.sh ./dist
```

The build matrix is for POSIX shells. On Windows, build directly with Go or set the target environment variables in PowerShell.

## 🔐 Security and profile handling

- Generated `.conf` profiles contain a private key. Keep them private and do not commit profiles, `.bak` backups, or keys to a public repository.
- Local proxies do not require authentication, so Oberon binds them to loopback addresses. They are intended for apps on the same computer.
- Proxy listeners carry TCP traffic and are not a network-wide proxy.

## 🧰 Troubleshooting

- **No profile found:** place a valid `.conf` beside `oberon`/`oberon.exe` or run Oberon from the directory containing the profile. The TUI creates one automatically on first connection.
- **No endpoint completes a handshake:** check network access, try `-mode all`, or scan again later. Endpoint availability can change.
- **Registration fails:** check DNS and access to the WARP registration service.
- **Windows access or adapter error:** approve the UAC prompt and confirm that the matching Wintun 0.14.1 DLL is beside the executable.
- **A proxy port is already in use:** choose another loopback port with `-socks` or `-http`.

When reporting an issue, include the OS, architecture, Oberon version, command used, and relevant error output. Remove private keys and account identifiers from logs and profiles first.

## 📜 License and notices

Oberon is distributed under the GNU Affero General Public License v3.0 only (`AGPL-3.0-only`). If you run a modified version as a network service, AGPL requires offering its Corresponding Source to users interacting with it remotely. Source files with separate copyright or SPDX notices remain subject to those notices. Oberon's name and branding are covered by `TRADEMARK.md`.

