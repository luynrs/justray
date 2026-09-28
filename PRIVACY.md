<img src=".github/assets/privacy.png" width="480" alt="privacy">

Privacy Policy  
Last updated: September 27, 2026

**JustRay** is an open-source, local-first terminal proxy and VPN client based on sing-box core (`justray` and daemon `justrayd`). We do not operate central servers, user accounts, or telemetry services.

### 1. Network Traffic and VPN Data
- **Connections:** Traffic routes directly from your device to your configured proxy or VPN server. We do not operate intermediate proxies or relays.
- **No logging:** We do not inspect, intercept, store, or modify your network packets. Traffic handling at the exit node is governed by your chosen provider's privacy policy.
- **Connection modes:**
  - **Proxy:** Runs locally on `127.0.0.1:10808` to forward application traffic.
  - **TUN:** Creates a virtual network adapter (`justray`) with administrator/root privileges to route system IP traffic.
- **Latency Testing:** `jray probe` sends test requests directly to target nodes to measure response times (default: `http://www.gstatic.com/generate_204`).

### 2. Information Sent to Subscription Providers
When you add or refresh remote subscriptions, JustRay sends HTTPS requests directly to your provider's URL:
- **Device information:** To support provider-enforced device limits, requests include your OS name, OS version, device model, and `X-Hwid` (SHA-256 hash of your machine ID and the subscription domain). Sent strictly to your subscription provider, never to JustRay.
- **Privacy protection:** Because the hostname is salted into the hash, providers cannot track your device across different services. Headers are stripped on cross-domain redirects.

### 3. Telemetry and Analytics
JustRay collects **no** telemetry, crash reports (including through Sentry), analytics, or usage statistics. No tracking data is transmitted. If you want to voluntarily provide information about a bug or feature request, please use GitHub Issues.

### 4. Local Storage
All data is stored in your local user profile:
- **Windows:** `%APPDATA%\justray\`
- **Linux:** `~/.config/justray/`
- **macOS:** `~/Library/Application Support/justray/`

Files include `config.json` (preferences), `state.json` (subscription URLs, node credentials), `logs/` (local troubleshooting logs), and `ipc/` (local socket for daemon control).

- **Deletion:** Remove subscriptions via `jray sub remove <id>`, clear files under `logs/`, or delete the `justray` folder to erase everything.

### 5. Contact
For privacy-related questions, please open an issue on the project's GitHub repository.
