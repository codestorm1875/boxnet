# Security model

## Current guarantees

- Node identities are persistent Ed25519 key pairs; the node ID is the SHA-256 fingerprint of the public key.
- Network traffic requires TLS 1.3. TLS 1.2 and older versions are refused.
- Every mesh derives an isolated Ed25519 certificate authority from its 256-bit invitation. Each node receives a CA-issued two-year certificate tied to its identity.
- Peer API routes require a valid mesh client certificate in addition to the signed application request. Dashboard clients use the same encrypted listener without requiring a client certificate.
- A 256-bit mesh invitation establishes membership. It is stored with owner-only filesystem permissions.
- Discovery announcements and manifests are signed and expire after 90 seconds.
- Peer requests carry a signature, timestamp, and one-use random nonce.
- Dashboard APIs require a separate 256-bit bearer token kept in browser session storage.
- Peer redirects are refused and peer origins cannot contain credentials, paths, queries, fragments, or non-HTTP schemes.
- Content is addressed and verified by SHA-256 before replication commits it.
- Upload size, total storage, manifest size, filename length, and concurrent transfers are bounded.
- Uploaded content is stored outside the web assets and peer blobs are served as downloads.

## Remaining risks

- The shared invitation has no per-node revocation mechanism. Rotate the mesh by creating a new data directory and rejoining trusted nodes if it leaks.
- Files are not encrypted at rest.
- Trusted members can publish malicious or illegal files. Boxnet does not inspect file content.
- Availability still depends on enough honest peers retaining replicas.

## Secret handling

Never commit or share `identity.key`, `mesh.key`, `admin.token`, or invitation files. Keep their permissions at `0600`. The exported `mesh-ca.crt` contains only the public CA certificate and may be installed on trusted administrator devices. Prefer `-join-file` over `-join`, because command-line arguments may be retained in shell history or visible in process listings.

## Reporting a vulnerability

Until a private reporting channel is configured, do not publish exploitable details or real mesh secrets in an issue. Reproduce problems with a disposable data directory and redact tokens, invitations, private keys, node addresses, and filenames from logs.
