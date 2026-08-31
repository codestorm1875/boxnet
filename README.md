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
  -advertise https://192.168.1.20:8080 \
  -data ./data/living-room
```

Open `https://localhost:8080`. Replace `192.168.1.20` with the machine's LAN address so other computers can reach it.

On first start, the terminal prints two secrets:

- **Dashboard token:** enter this in the GUI unlock screen. It is also stored in `data/living-room/admin.token` with owner-only permissions.
- **Mesh invitation:** transfer this once, through a trusted channel, to each computer that should join the mesh.

Boxnet exports the mesh's public certificate authority to `data/living-room/mesh-ca.crt`. Import that certificate into the browser or operating-system trust store on computers that administer the mesh. Until it is trusted, the browser will show a certificate warning; verify the certificate came directly from your own node before accepting it.

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
go run . -name "Alpha" -listen :8080 -advertise https://127.0.0.1:8080 -data ./data/alpha
```

Copy Alpha's printed `boxnet1.…` invitation into a file named `alpha.invite`, then restrict it:

```bash
chmod 600 alpha.invite
```

```bash
go run . -name "Beta" -listen :8081 -advertise https://127.0.0.1:8081 -data ./data/beta -peers https://127.0.0.1:8080 -join-file ./alpha.invite
```

Open `https://localhost:8080` and `https://localhost:8081`. Publish a file on Alpha; Beta will pull it during the next synchronization cycle. Stop Alpha and the file remains downloadable from Beta. Start Alpha again and both nodes reconcile automatically.

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
| `-advertise` | `https://localhost:8080` | Reachable HTTPS URL announced to peers and included in the certificate |
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
3. Nodes establish TLS 1.3 using certificates issued by their mesh-specific CA. Peer endpoints require a mutually authenticated client certificate.
4. Authorized peers exchange signed manifests over authenticated `GET /api/manifest` requests. Timestamped, one-use nonces prevent request replay.
5. A node requests missing blobs through authenticated `GET /api/blobs/{sha256}` calls.
6. The received bytes are hashed before they are committed to local storage.
7. If a node disappears, local files remain available. When connectivity returns, authenticated manifest reconciliation resumes.

## Trust and scope

Boxnet authenticates dashboard operations, gives every node a persistent Ed25519 identity, admits peers through a secret invitation, signs discovery/manifests/peer requests, rejects replayed requests and redirects, limits storage and concurrent transfers, and hardens browser responses.

All dashboard and replication traffic is encrypted with TLS 1.3. Peers additionally use mutual TLS certificates and signed application requests. Keep Boxnet on a private network: files are not encrypted at rest yet, trusted members can still publish malicious content, and the shared invitation does not yet support per-node revocation.
