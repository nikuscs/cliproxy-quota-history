# cliproxy-quota-history

A CLIProxyAPI plugin that reads every Claude, Codex and xAI credential's quota on
the server every few minutes and keeps the readings, so the
[panel](https://github.com/nikuscs/cliproxy-panel) has history for the hours no
browser tab was open.

## Install

```sh
sh scripts/build.sh 0.1.0   # dist/quota-history-v0.1.0.dylib (.so on Linux)
cp dist/quota-history-v0.1.0.* <cpa plugins dir>/
```

Then enable it in the CPA config and restart CPA:

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    quota-history:
      enabled: true
      poll_interval_seconds: 300 # default 300, minimum 60
      retention_days: 30         # default 30
```

Build it on the machine that runs CPA, against the same CPA version (`go.mod`).

## API

Both routes need the management key.

- `GET /v0/management/quota-history/status`: poller state and the last errors per credential.
- `GET /v0/management/quota-history/readings?since=<unix ms>`: stored readings, oldest first.

A reading is one window of one credential:

```json
{ "ts": 1761321600000, "credential": "claude-user.json", "provider": "claude",
  "window": "seven-day", "remaining": 61, "reset_at_ms": 1761400000000, "period_hours": 168 }
```

Readings live in `<plugins dir>/data/quota-history/YYYY-MM-DD.jsonl`.

## License

AGPL-3.0. The native bridge, host callbacks, egress and quota parsing are
adapted from [Lamplighter](https://github.com/PosvdM/cpa-plugin-lamplighter).
