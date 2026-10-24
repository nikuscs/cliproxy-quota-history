# 📈 Quota History

> A CLIProxyAPI plugin that records your Claude, Codex and xAI quota on the server, so the panel's forecast has history even when no tab is open.

The [CLIProxyAPI panel](https://github.com/nikuscs/cliproxy-panel) forecasts when each account runs out, but on its own it only records quota while a browser tab is open. This plugin runs inside CLIProxyAPI, reads every credential's quota every few minutes, and keeps the readings on the server. The panel picks them up automatically, so nights and weekends are no longer gaps in the chart.

## ✨ Features

- ⏱️ **Background polling.** Reads every enabled credential every 5 minutes (configurable, minimum 1 minute), with no tab open.
- 🤖 **Claude, Codex and xAI.** Claude 5-hour, 7-day and Fable windows, Codex weekly and 5-hour windows, and the xAI weekly credit window.
- 🎯 **Same numbers as the panel.** Uses the same endpoints, headers and window IDs as the panel's quota page, so server and browser readings line up.
- 💾 **Plain files.** One JSON Lines file per day, kept for 30 days and pruned automatically.
- 🔌 **Zero setup in the panel.** The panel finds the plugin by itself; without it, the panel keeps working with its own history.
- 🔐 **Tokens stay put.** Access tokens are read from CLIProxyAPI per request and sent only to the provider's own quota endpoint. They are never stored, logged or returned.
- 🩺 **Easy to check.** Writes a `status.json` with the last poll and any errors next to the readings.

## 🧰 Built with

[CLIProxyAPI plugin SDK](https://github.com/router-for-me/CLIProxyAPI) · [Go](https://go.dev) · cgo (`c-shared`) · [yaml.v3](https://github.com/go-yaml/yaml)

## 🚀 Install in 3 steps

**You need:** CLIProxyAPI v8 with plugin support, and Go on the machine that runs it.

**1. Build**

```bash
git clone https://github.com/nikuscs/cliproxy-quota-history && cd cliproxy-quota-history
sh scripts/build.sh 0.1.0
```

This builds `dist/quota-history-v0.1.0.dylib` on macOS (`.so` on Linux). Build against the same CLIProxyAPI version you run (see `go.mod`).

**2. Copy it into the plugins folder**

```bash
cp dist/quota-history-v0.1.0.* <plugins dir>/
```

**3. Enable it and restart CLIProxyAPI**

```yaml
plugins:
  enabled: true
  dir: "<plugins dir>"
  configs:
    quota-history:
      enabled: true
      poll_interval_seconds: 300 # default 300, minimum 60
      retention_days: 30         # default 30
      data_dir: "<data dir>"     # default <plugins dir>/data/quota-history
```

> [!TIP]
> If CLIProxyAPI runs as a service with `/` as its working directory (Homebrew does), use absolute paths for `dir` and `data_dir`.

## 🩺 Check it works

A minute after the restart:

```bash
cat <data dir>/status.json     # last_poll_at_ms is recent, errors is {}
wc -l <data dir>/$(date -u +%F).jsonl   # grows every poll
```

In the panel, open DevTools → Network on the quota page: `quota-history/readings` returns `200`.

## 📡 API

Both routes need the management key.

| Route | Returns |
| --- | --- |
| `GET /v0/management/quota-history/status` | Poller state, last poll and errors per credential |
| `GET /v0/management/quota-history/readings?since=<unix ms>` | Stored readings, oldest first |

A reading is one window of one credential:

```json
{ "ts": 1761321600000, "credential": "claude-user.json", "provider": "claude",
  "window": "seven-day", "remaining": 61, "reset_at_ms": 1761400000000, "period_hours": 168 }
```

> [!NOTE]
> Readings name each credential by its file name, which often contains the account email. They stay on your server and are only returned with the management key.

## 📄 License

AGPL-3.0. The native bridge, host callbacks, proxy handling and quota parsing are adapted from [Lamplighter](https://github.com/PosvdM/cpa-plugin-lamplighter) by PosvdM.
