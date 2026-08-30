# Boxnet — Internet in a Box

Boxnet is a small, dependency-free local mesh for sharing and replicating files without a central server. Each computer runs the same binary, keeps its own content-addressed store, discovers other nodes on the LAN, and reconciles missing files when connections return.

## What works

- Automatic peer discovery over UDP multicast on the local network
- Optional seed peers for networks where multicast is unavailable
- Immutable SHA-256-addressed file storage
- Automatic pull replication and integrity verification
- Persistent node identity, metadata, and file blobs
- Continued local browsing and downloads while every peer is offline
- Automatic catch-up when a node rejoins
- Responsive embedded operations dashboard
- No runtime dependencies, database, cloud account, or internet connection

## Start one node

```bash
go run . \
  -name "Living Room" \
  -listen :8080 \
  -advertise http://192.168.1.20:8080 \
  -data ./data/living-room
```

Open `http://localhost:8080`. Replace `192.168.1.20` with the machine's LAN address so other computers can reach it.

## GUI and TUI

The same binary now supports four interface modes:

```bash
# Start the node and open its graphical dashboard in your default browser
go run . -ui browser -name "Living Room"

# Run the live terminal dashboard while keeping the web API available
go run . -ui tui -name "Living Room"

# Serve the graphical dashboard without opening a browser (the default)
go run . -ui web

# Headless node for a server or service manager
go run . -ui none
```

The terminal interface refreshes every five seconds and accepts these commands:

| Command | Action |
| --- | --- |
| `add /path/to/file` | Publish a local file to the mesh |
| `save 1 ./copy.ext` | Export file number 1 without overwriting an existing file |
| `sync` or `s` | Start peer reconciliation immediately |
| `refresh` or `r` | Redraw current state |
| `help` or `h` | Show available commands |
| `quit` or `q` | Gracefully stop the node |

## Try two nodes on one computer

Run these in separate terminals:

```bash
go run . -name "Alpha" -listen :8080 -advertise http://localhost:8080 -data ./data/alpha
```

```bash
go run . -name "Beta" -listen :8081 -advertise http://localhost:8081 -data ./data/beta -peers http://localhost:8080
```

Open `http://localhost:8080` and `http://localhost:8081`. Publish a file on Alpha; Beta will pull it during the next synchronization cycle. Stop Alpha and the file remains downloadable from Beta. Start Alpha again and both nodes reconcile automatically.

## Build and test

```bash
go test ./...
go build -o boxnet .
```

The binary serves both the API and dashboard. Useful flags:

| Flag | Default | Purpose |
| --- | --- | --- |
| `-name` | `Home Node` | Name shown to people on the mesh |
| `-listen` | `:8080` | Local HTTP bind address |
| `-advertise` | `http://localhost:8080` | Reachable URL announced to peers |
| `-data` | `./box-data` | Persistent identity, index, and blobs |
| `-replicas` | `2` | Desired availability target shown by the dashboard |
| `-peers` | empty | Comma-separated peer URLs used as discovery seeds |
| `-ui` | `web` | Interface mode: `web`, `browser`, `tui`, or `none` |

## Protocol overview

1. A node broadcasts a small `boxnet/1` presence message to `239.255.67.89:9797` every three seconds.
2. Peers exchange manifests over `GET /api/manifest`.
3. A node requests any missing blob through `GET /api/blobs/{sha256}`.
4. The received bytes are hashed before they are committed to local storage.
5. If a node disappears, local files remain available. When connectivity returns, manifest reconciliation resumes.

## Trust and scope

This MVP is designed for a trusted private LAN. Its HTTP API has no authentication or transport encryption, so do not expose it directly to the public internet. A production evolution should add signed node identities, encrypted transport, explicit mesh invitations, storage quotas, tombstones, and configurable replica placement.
