<img src=".github/assets/privacy.png" width="480" alt="privacy">

Privacy Policy  
Last updated: October 13, 2026

JustRay is a local proxy and VPN client. The project operates no user accounts, traffic relays, or telemetry services.

### 1. Network Traffic

Traffic follows your configured proxy, VPN, and direct routing rules. Remote traffic is subject to the selected provider's privacy policy.

Proxy mode listens on `127.0.0.1:10808` by default. TUN mode uses a virtual network adapter. Latency probes request the configured test URL through each tested node; the default is `http://www.gstatic.com/generate_204`.

### 2. Subscription Requests

Remote subscriptions are fetched over HTTP or HTTPS. Requests include the JustRay version and `X-Hwid`. OS name, OS version, and device model are included when available.

`X-Hwid` is a 128-bit identifier derived from the machine ID and request hostname using SHA-256. It varies by hostname and does not guarantee anonymity.

Cross-host redirects remove the JustRay version and OS/model headers and regenerate `X-Hwid` for the destination hostname.

### 3. Diagnostics

Local logs may contain connection destinations, DNS queries, errors, and crash diagnostics. JustRay does not upload these logs or send telemetry, analytics, usage statistics, or crash reports to collection services.

### 4. Local Storage

Settings, subscriptions, credentials, and logs are stored under:

- Windows: `%APPDATA%\justray\`
- Linux: `$XDG_CONFIG_HOME/justray/`, defaulting to `~/.config/justray/`
- macOS: `~/Library/Application Support/justray/`

Files include `config.json` (preferences), `state.json` (subscription URLs and node credentials), `logs/` (diagnostics), and `ipc/` (daemon control socket).

Remove subscriptions with `jray sub remove <id>`. Delete files under `logs/` to clear diagnostics, or delete the data directory to remove local records.

### 5. Contact

Privacy questions can be submitted through the project's GitHub Issues.
