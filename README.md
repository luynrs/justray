<p align="center">
  <img src=".github/assets/logotype.png" width="480" alt="justray">
</p>

<p align="center">
  <a href="https://github.com/luynrs/justray/commits/main"><img alt="Last commit" src="https://img.shields.io/github/last-commit/luynrs/justray?style=for-the-badge&logo=github&logoColor=white&labelColor=1e1e2e&color=cba6f7"></a>
  <img alt="Platforms" src="https://img.shields.io/badge/platform-linux%20%7C%20macos%20%7C%20windows-cba6f7?style=for-the-badge&logo=linux&logoColor=white&labelColor=1e1e2e">
  <a href="https://github.com/luynrs/justray/releases"><img alt="Version" src="https://img.shields.io/github/v/release/luynrs/justray?style=for-the-badge&labelColor=1e1e2e&color=cba6f7"></a>
</p>

<p align="center">
  A modern VPN/proxy client that lives in your terminal
</p>

<p align="center">
  <img src=".github/assets/tui.gif" width="100%" alt="meow >.<">
</p>

### Features

- **Modern protocols:** VMess, VLESS, Trojan, Shadowsocks, Hysteria 1/2, TUIC, AnyTLS, SOCKS5, and more
- **Flexible:** import subscriptions from raw links or Clash/Mihomo YAML, with automatic refresh and a wide range of settings
- **Headless:** the daemon and embedded sing-box core run independently from the TUI, keeping connections alive after you detach
- **Lightweight:** ~50 MB RAM on Linux/macOS and ~100 MB on Windows
- **Cross-platform:** runs in modern terminals on Linux, macOS, and Windows, including native PowerShell and WSL

### Installation

Using package manager:

```bash
# macOS or Linux
brew install luynrs/tap/justray

# Arch Linux (AUR)
yay -S justray-bin

# Windows
winget install luynrs.justray
```

Alternatively, with [x-cmd](https://www.x-cmd.com/mod/eget):

```bash
x eget use luynrs/justray
```

Or via script:

```bash
# Linux / macOS
curl -fsSL https://raw.githubusercontent.com/luynrs/justray/main/install.sh | sh
```

```powershell
# Windows
irm https://raw.githubusercontent.com/luynrs/justray/main/install.ps1 | iex
```

Nix:

```bash
# Run directly
nix run github:luynrs/justray
```

```nix
# Home Manager module (justrayd systemd user service)
{
  imports = [ justray.homeManagerModules.default ];
  services.justray.enable = true;
}
```

### Usage

Package manager and script installations also provide `jray` as a short alias for `justray`

> [!NOTE]
> WinGet provides only `justray` and `justrayd`; the `jray` alias must be added manually

- `jray`: open the TUI

#### Connection Control (CLI)

- `jray up <node> [--tun | --proxy]`: start the daemon and connect
- `jray down`: disconnect
- `jray stop`: shut down the daemon
- `jray status`: show connection status

#### Subscriptions & Nodes

`jray subscription` or `jray sub`

- `add`: add a subscription or raw protocol link
- `remove`: remove a subscription by ID or name
- `list`: list subscriptions and nodes

#### General Options

- `-h`, `--help`: show help
- `-v`, `--version`: show the current version

### Keybinds (TUI)

| **Key**      | **Action**                | **Key**       | **Action**             |
| ------------ | ------------------------- | ------------- | ---------------------- |
| `↑/↓`, `k/j` | Navigate list             | `shift+↑/↓`   | Reorder subscriptions  |
| `←/→`, `h/l` | Fold / expand group       | `Enter`       | Toggle group / connect |
| `t` / `T`    | Ping selected / Ping all  | `r` / `R`     | Refresh selected / all |
| `m`          | Switch mode (PROXY / TUN) | `/`           | Filter nodes           |
| `a`          | Add subscription / node   | `d`           | Delete subscription    |
| `o`          | Settings menu             | `Esc`         | Back / cancel input    |
| `Tab`        | Switch active panel       | `q`, `Ctrl+C` | Detach / exit TUI      |

### License
[GPL-3.0](LICENSE)
