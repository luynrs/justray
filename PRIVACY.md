<img src=".github/assets/privacy.png" width="480" alt="privacy">

# Privacy Policy

Last updated: October 67, 2026

JustRay is an open-source VPN and proxy client for Linux, macOS and Windows. It does not provide servers, subscriptions or user accounts.

## Data collection

JustRay does not send telemetry, usage statistics or crash reports to the project. Your settings, subscriptions, credentials and logs are stored on your device.

## Local data

JustRay stores its data in:

| Platform | Location |
| --- | --- |
| Linux | `$XDG_CONFIG_HOME/justray/` or `~/.config/justray/`|
| macOS | `~/Library/Application Support/justray/` |
| Windows | `%APPDATA%\justray\` |

This directory contains your settings, subscription URLs, node credentials and logs. Logs may include server addresses, DNS queries and errors. Review files and remove sensitive information before sharing them.

## Network connections

- **Proxy traffic:** Traffic follows your routing settings and may pass through the server you choose. Your provider's privacy policy applies.
- **Subscriptions:** JustRay requests the subscription URLs you add. The provider can see the request and its source IP address. Automatic refresh defaults to six hours and can be changed or disabled in settings.
- **Device information:** Subscription requests include the JustRay version and `X-Hwid`, an identifier derived from your machine ID and the request hostname. OS name, OS version and device model are included when available. The identifier differs between hostnames and does not guarantee anonymity. On a redirect to a different host, JustRay removes its version and device information headers and generates an identifier for the destination hostname.
- **DNS:** JustRay uses `8.8.8.8` by default. The DNS provider can see the domain names it resolves. You can change the server in settings.
- **Latency tests:** When you test nodes, JustRay sends a request through each node to `http://www.gstatic.com/generate_204` by default. You can change the test URL in settings.
- **Local proxy:** The proxy listens on `127.0.0.1:10808` by default. You can change the port or allow access from your local network in settings.

## Removing data

You can remove subscriptions from the TUI or with `jray sub remove <id>`. To remove all local settings, subscriptions, credentials and logs, stop the daemon with `justray stop`, then delete the JustRay data directory. You can also delete the files in `logs/` to clear diagnostics.

Autostart is managed by your operating system and can be disabled in JustRay's settings. Removing local data does not remove information held by your providers.

## Contact

For privacy questions, open an issue at https://github.com/luynrs/justray/issues. Issues are public, so do not include secrets or unreviewed logs.
