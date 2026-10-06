<p align="center">
  <img src="../assets/logotype.png" width="480" alt="justray">
</p>

<p align="center">
  <a href="../../README.md">English</a> / <a href="README.RU.md">Русский</a> / <a href="README.CN.md">简体中文</a>
</p>

<p align="center">
  <a href="https://github.com/luynrs/justray/commits/main"><img alt="最近提交" src="https://img.shields.io/github/last-commit/luynrs/justray?style=for-the-badge&logo=github&logoColor=white&labelColor=1e1e2e&color=cba6f7"></a>
  <img alt="支持的平台" src="https://img.shields.io/badge/platform-linux%20%7C%20macos%20%7C%20windows-cba6f7?style=for-the-badge&logo=linux&logoColor=white&labelColor=1e1e2e">
  <a href="https://github.com/luynrs/justray/releases"><img alt="版本" src="https://img.shields.io/github/v/release/luynrs/justray?style=for-the-badge&labelColor=1e1e2e&color=cba6f7"></a>
</p>

<p align="center">
  住在终端里的 VPN 客户端
</p>

<p align="center">
  <img src="../assets/tui.gif" width="100%" alt="justray 界面">
</p>

### 功能

- **协议支持：** VMess、VLESS、Trojan、WireGuard、Shadowsocks、Hysteria 1/2、TUIC、AnyTLS、SOCKS5 等
- **导入与设置：** 可导入节点链接，以及 Clash/Mihomo YAML、sing-box 或 Xray JSON 格式的订阅，支持自动更新和多种设置
- **后台运行：** 后台服务和内嵌的 sing-box 核心独立于 TUI 运行，关闭界面后仍保持连接
- **内存占用：** Linux/macOS 上约 50 MB，Windows 上约 100 MB
- **平台支持：** 可在 Linux、macOS 13+ 和 Windows 1803+ 的现代终端中运行，包括原生 PowerShell 和 WSL

### 安装

使用包管理器：

```bash
# macOS 或 Linux
brew install luynrs/tap/justray

# Arch Linux (AUR)
yay -S justray-bin

# Windows
winget install luynrs.justray
```

也可以使用 [x-cmd](https://www.x-cmd.com/mod/eget)：

```bash
x eget use luynrs/justray
```

或通过脚本安装：

```bash
# Linux / macOS
curl -fsSL https://raw.githubusercontent.com/luynrs/justray/main/install.sh | sh
```

```powershell
# Windows
irm https://raw.githubusercontent.com/luynrs/justray/main/install.ps1 | iex
```

Nix：

Flake 支持 Linux（x86_64/aarch64）和 Apple Silicon。Intel Mac 请使用 Homebrew 或预编译的二进制文件

```bash
# 直接运行
nix run github:luynrs/justray
```

```nix
# Home Manager
{
  imports = [ justray.homeManagerModules.default ];
  services.justray.enable = true;
}
```

### 使用

通过包管理器或脚本安装时，还会提供 `jray`，作为 `justray` 的简写

> [!NOTE]
> WinGet 只提供 `justray` 和 `justrayd`；需要手动添加 `jray` 别名

- `jray`：打开 TUI

#### 连接

- `jray up <node> [--tun | --proxy]`：启动后台服务并连接
- `jray down`：断开连接
- `jray stop`：停止后台服务
- `jray probe [sub | id | name]`：测量节点延迟
- `jray status [--json]`：查看连接状态
- `jray logs [daemon | engine | tui] [-f]`：查看日志

#### 订阅与节点

`jray subscription` 或 `jray sub`

- `add`：添加订阅或节点链接
- `remove`：按 ID 或名称删除订阅
- `refresh`：更新订阅
- `list [--json]`：列出订阅和节点

#### 其他选项

- `-h`、`--help`：显示帮助
- `-v`、`--version`：显示当前版本

### 快捷键

| **按键** | **操作** | **按键** | **操作** |
| ------------ | ------------ | ------------ | ------------ |
| `↑/↓`、`k/j` | 移动光标 | `Shift+↑/↓` | 调整订阅顺序 |
| `←/→`、`h/l` | 折叠 / 展开 | `Enter` | 连接 / 断开连接 |
| `t` / `T` | 测量选中节点 / 所有节点的延迟 | `r` / `R` | 更新选中订阅 / 所有订阅 |
| `m` | 切换模式（PROXY / TUN） | `/` | 筛选节点 |
| `a` | 添加订阅 / 节点 | `d` | 删除订阅 |
| `o` | 打开设置 | `Esc` | 返回 / 取消 |
| `Tab` | 切换面板 | `q`、`Ctrl+C` | 退出 TUI |

### 许可证

[GPL-3.0-or-later](../../LICENSE)
