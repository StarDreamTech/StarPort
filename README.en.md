# StarPort

[简体中文](README.md) | [English](README.en.md)

<img src="assets/brand/starport.svg" alt="StarPort" width="96" height="96">

Give selected nodes from a local Clash profile their own loopback SOCKS ports. The management page lets you add, remove, start and stop exits. StarPort manages only the mihomo process it starts; it leaves Clash Verge's system proxy and TUN untouched.

## Run locally
```bash
go build -o starport ./cmd/starport
cp config.example.yaml config.yaml
./starport
```
Open [the local dashboard](http://127.0.0.1:18765). If `profile` is empty, StarPort reads the profile selected in Clash Verge. The interface shows node display names, not subscription links. The generated `data/runtime.yaml` contains only selected nodes and has mode `0600`.

## Local API
The management listener is loopback-only and has no separate password. Do not expose it to a network.

| Request | Result |
| --- | --- |
| `GET /api/status` | Process status, saved exits and ports actually listening |
| `GET /api/catalog` | Node display names, excluding subscription notices |
| `POST /api/exits` | Save an exit, e.g. `{"name":"NODE_DISPLAY_NAME","port":18008}`. Port `0` selects a free port starting at `18001`. |
| `DELETE /api/exits/{port}` | Remove a saved exit |
| `POST /api/start` | Apply the saved list; restart the managed process if already running |
| `POST /api/stop` | Stop the managed process |
| `GET /api/agents` | Active addresses such as `socks5h://127.0.0.1:18008` |

Changes to the list take effect on Start. With `socks5h`, the proxy resolves the hostname rather than the local system.

## Idle memory measurements
Mihomo Meta v1.19.33 on Darwin ARM64, about 1.5 seconds after startup, before forwarding traffic. RSS is resident memory from `ps`; footprint comes from `vmmap`.

| Configuration | Ports | RSS (KB) | Physical footprint (MB) |
| --- | ---: | ---: | ---: |
| DIRECT | 0 | 31280 | 13.4 |
| DIRECT | 1 | 31728 | 13.3 |
| DIRECT | 10 | 32144 | 13.5 |
| DIRECT | 50 | 33904 | 14.8 |
| DIRECT | 91 | 34720 | 15.5 |
| Real nodes, DNS off | 1 | 32400 | 13.5 |
| Real nodes, DNS off | 3 | 32624 | 13.6 |
| Real nodes, DNS off | 91 | 36048 | 16.5 |

Adding 90 real exits cost about 41 KB RSS per exit in this idle test. A separate, warmed 91-port process reached a 25 MB footprint after accessing the geodata. These are not per-connection measurements under traffic.

## Keep private
`config.yaml` holds machine-specific paths; `data/` holds selected ports, generated configs, logs and PID files. Both are Git-ignored. Do not commit subscription URLs, node UUIDs, passwords or server addresses.
