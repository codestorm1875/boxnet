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

On first start, the terminal prints two secrets:

- **Dashboard token:** enter this in the GUI unlock screen. It is also stored in `data/living-room/admin.token` with owner-only permissions.
- **Mesh invitation:** transfer this once, through a trusted channel, to each computer that should join the mesh.

## GUI and TUI

The same binary supports four interface modes:

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

The terminal interface accepts these commands:

| Command | Action |
| --- | --- |
| `add /path/to/file` | Publish a local file to the mesh |
| `save 1 ./copy.ext` | Export file number 1 without overwriting an existing file |
| `sync` or `s` | Start peer reconciliation immediately |
| `invite` | Show the secret invitation used to add a trusted node |
| `token` | Show the token used to unlock this node's web dashboard |
| `refresh` or `r` | Redraw current state |
| `help` or `h` | Show available commands |
| `quit` or `q` | Gracefully stop the node |

## Try two nodes on one computer

Run these in separate terminals:

```bash
go run . -name "Alpha" -listen :8080 -advertise http://127.0.0.1:8080 -data ./data/alpha
```

Copy Alpha's printed `boxnet1.…` invitation into a file named `alpha.invite`, then restrict it:

```bash
chmod 600 alpha.invite
```

```bash
go run . -name "Beta" -listen :8081 -advertise http://127.0.0.1:8081 -data ./data/beta -peers http://127.0.0.1:8080 -join-file ./alpha.invite
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
| `-join` | empty | Mesh invitation; convenient but may remain in shell history |
| `-join-file` | empty | Preferred path to an owner-readable invitation file |
| `-show-invite` | `false` | Print the existing mesh invitation |
| `-max-upload` | 1 GiB | Maximum bytes accepted for one file |
| `-max-storage` | 10 GiB | Maximum bytes retained by this node |

## Protocol overview

1. A node broadcasts a signed `boxnet/2` presence message to `239.255.67.89:9797` every three seconds.
2. Nodes validate the sender address, Ed25519 identity, mesh-membership proof, timestamp, and signature.
3. Authorized peers exchange signed manifests over authenticated `GET /api/manifest` requests. Timestamped, one-use nonces prevent request replay.
4. A node requests missing blobs through authenticated `GET /api/blobs/{sha256}` calls.
5. The received bytes are hashed before they are committed to local storage.
6. If a node disappears, local files remain available. When connectivity returns, authenticated manifest reconciliation resumes.

## Trust and scope

Boxnet now authenticates dashboard operations, gives every node a persistent Ed25519 identity, admits peers through a secret invitation, signs discovery/manifests/peer requests, rejects replayed requests and redirects, limits storage and concurrent transfers, and hardens browser responses.

Transport is **not encrypted yet**. A device capable of observing LAN traffic can see filenames and file contents, and could capture a dashboard token. Keep Boxnet on a trusted private LAN and do not expose it directly to the public internet. The next security milestone is TLS 1.3 with mutually authenticated peer certificates, followed by encrypted-at-rest blobs and revocable membership.
