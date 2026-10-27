<p align="center">
  <img src="../assets/logotype.png" width="480" alt="justray">
</p>

<p align="center">
  <a href="../../README.md">English</a> / <a href="README.RU.md">Русский</a> / <a href="README.CN.md">简体中文</a>
</p>

<p align="center">
  <a href="https://github.com/luynrs/justray/commits/main"><img alt="Последний коммит" src="https://img.shields.io/github/last-commit/luynrs/justray?style=for-the-badge&logo=github&logoColor=white&labelColor=1e1e2e&color=cba6f7"></a>
  <img alt="Платформы" src="https://img.shields.io/badge/platform-linux%20%7C%20macos%20%7C%20windows-cba6f7?style=for-the-badge&logo=linux&logoColor=white&labelColor=1e1e2e">
  <a href="https://github.com/luynrs/justray/releases"><img alt="Версия" src="https://img.shields.io/github/v/release/luynrs/justray?style=for-the-badge&labelColor=1e1e2e&color=cba6f7"></a>
</p>

<p align="center">
  Современный VPN-клиент, обитающий в вашем терминале
</p>

<p align="center">
  <img src="../assets/tui.gif" width="100%" alt="Интерфейс justray">
</p>

### Возможности

- **Поддерживаемые протоколы:** VMess, VLESS, Trojan, WireGuard/AmneziaWG, Shadowsocks, Hysteria 1/2, TUIC, AnyTLS, SOCKS5 и другие
- **Гибкость:** импорт подписок из ссылок, Clash/Mihomo YAML, sing-box или Xray JSON с автоматическим обновлением и широким выбором настроек
- **Маршрутизация по правилам:** правила для приложений, доменов, wildcard-шаблонов и GeoIP/Geosite
- **Автономность:** демон и встроенное ядро sing-box работают независимо от TUI, сохраняя подключение после закрытия интерфейса
- **Легковесность:** около 50 МБ RAM на Linux / MacOS и 100 МБ на Windows
- **Кроссплатформенность:** работает в современных терминалах на Linux, MacOS 13+ и Windows 1803+

### Установка

Через пакетный менеджер:

```bash
# Linux / MacOS
brew install luynrs/tap/justray
```

```bash
# Arch Linux (AUR)
yay -S justray-bin
```

```powershell
# Windows
winget install luynrs.justray
```

С использованием [x-cmd](https://www.x-cmd.com/mod/eget):

```bash
x eget use luynrs/justray
```

Или через скрипт:

```bash
# Linux / MacOS
curl -fsSL https://raw.githubusercontent.com/luynrs/justray/main/install.sh | sh
```

```powershell
# Windows
irm https://raw.githubusercontent.com/luynrs/justray/main/install.ps1 | iex
```

Nix:

Модуль Home Manager запускает демон на Linux; на MacOS он только устанавливает пакет

```bash
nix run github:luynrs/justray
```

```nix
# Home Manager
{
  imports = [ justray.homeManagerModules.default ];
  services.justray.enable = true;
}
```

### Использование

При установке через пакетный менеджер или скрипт также доступен `jray` как короткий алиас для `justray`

> [!NOTE]
> WinGet устанавливает только `justray` и `justrayd`; алиас `jray` нужно добавить вручную

- `justray`: открыть TUI

#### Подключение

- `justray up <node> [--tun | --proxy]`: запустить и подключиться
- `justray down`: отключиться
- `justray stop`: остановить демон
- `justray probe [sub | id | name]`: измерить пинг
- `justray status [--json]`: показать состояние подключения
- `justray logs [daemon | engine] [-f]`: просмотреть логи

#### Подписки и узлы

`justray subscription` или `justray sub`

- `add`: добавить подписку или ссылку на узел
- `remove`: удалить подписку по ID или имени
- `refresh`: обновить подписки
- `list [--json]`: вывести список подписок и узлов

#### Дополнительно

- `-h`, `--help`: показать справку
- `-v`, `--version`: показать текущую версию

### Горячие клавиши

| **Кнопка** | **Действие** | **Кнопка** | **Действие** |
| ------------ | ------------ | ------------ | ------------ |
| `↑/↓`, `k/j` | Навигация | `Shift+↑/↓` | Изменить порядок подписок |
| `←/→`, `h/l` | Свернуть / развернуть | `Enter` | Подключиться / отключиться |
| `t` / `T` | Проверить задержку выбранного / всех узлов | `r` / `R` | Обновить выбранную / все подписки |
| `m` | Переключить режим (PROXY / TUN) | `/` | Фильтровать узлы |
| `a` | Добавить подписку / узел | `d` | Удалить подписку |
| `o` | Открыть настройки | `Esc` | Сохранить настройки / отменить ввод |
| `Tab` | Переключить вкладку настроек | `q`, `Ctrl+C` | Выйти из TUI |

### Лицензия

[GPL-3.0-or-later](../../LICENSE)
