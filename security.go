package main

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const invitePrefix = "boxnet1."

type nodeSecurity struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	meshKey    []byte
	adminToken string
	newMesh    bool
}

func loadSecurity(dataDir, invite string) (*nodeSecurity, error) {
	seed, err := loadOrCreateBytes(filepath.Join(dataDir, "identity.key"), ed25519.SeedSize)
	if err != nil {
		return nil, fmt.Errorf("identity: %w", err)
	}
	identity := ed25519.NewKeyFromSeed(seed)
	meshPath := filepath.Join(dataDir, "mesh.key")
	var meshKey []byte
	if invite != "" {
		meshKey, err = parseInvite(invite)
		if err != nil {
			return nil, err
		}
		if existing, readErr := os.ReadFile(meshPath); readErr == nil && !hmac.Equal(existing, meshKey) {
			return nil, errors.New("invitation does not match the mesh already configured in this data directory")
		}
	}
	newMesh := false
	if len(meshKey) == 0 {
		meshKey, err = os.ReadFile(meshPath)
		if errors.Is(err, os.ErrNotExist) {
			meshKey, err = randomBytes(32)
			newMesh = err == nil
		} else if err == nil && len(meshKey) != 32 {
			err = errors.New("mesh key has an invalid length")
		}
	}
	if err != nil {
		return nil, fmt.Errorf("mesh key: %w", err)
	}
	if err := writeSecret(meshPath, meshKey); err != nil {
		return nil, err
	}
	adminBytes, err := loadOrCreateBytes(filepath.Join(dataDir, "admin.token"), 32)
	if err != nil {
		return nil, fmt.Errorf("admin token: %w", err)
	}
	return &nodeSecurity{privateKey: identity, publicKey: identity.Public().(ed25519.PublicKey), meshKey: meshKey, adminToken: base64.RawURLEncoding.EncodeToString(adminBytes), newMesh: newMesh}, nil
}

func loadOrCreateBytes(path string, size int) ([]byte, error) {
	value, err := os.ReadFile(path)
	if err == nil {
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(value)))
		if decodeErr != nil || len(decoded) != size {
			return nil, errors.New("secret file is malformed")
		}
		return decoded, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	value, err = randomBytes(size)
	if err != nil {
		return nil, err
	}
	if err := writeSecret(path, []byte(base64.RawURLEncoding.EncodeToString(value))); err != nil {
		return nil, err
	}
	return value, nil
}

func writeSecret(path string, value []byte) error {
	if err := os.WriteFile(path, value, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func randomBytes(size int) ([]byte, error) {
	value := make([]byte, size)
	_, err := rand.Read(value)
	return value, err
}

func nodeID(publicKey ed25519.PublicKey) string {
	hash := sha256.Sum256(publicKey)
	return "bn-" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(hash[:]))
}

func (n *Node) Invite() string {
	return invitePrefix + base64.RawURLEncoding.EncodeToString(n.meshKey)
}

func parseInvite(invite string) ([]byte, error) {
	if !strings.HasPrefix(invite, invitePrefix) {
		return nil, errors.New("invalid invitation format")
	}
	key, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(invite, invitePrefix))
	if err != nil || len(key) != 32 {
		return nil, errors.New("invalid invitation key")
	}
	return key, nil
}

func (n *Node) membershipProof(publicKey []byte) string {
	mac := hmac.New(sha256.New, n.meshKey)
	_, _ = mac.Write(publicKey)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (n *Node) isMember(publicKey []byte, proof string) bool {
	provided, err := base64.RawURLEncoding.DecodeString(proof)
	if err != nil {
		return false
	}
	expected, _ := base64.RawURLEncoding.DecodeString(n.membershipProof(publicKey))
	return hmac.Equal(provided, expected)
}

func (n *Node) signPeerRequest(request *http.Request) error {
	nonce, err := randomBytes(18)
	if err != nil {
		return err
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonceText := base64.RawURLEncoding.EncodeToString(nonce)
	message := strings.Join([]string{request.Method, request.URL.EscapedPath(), timestamp, nonceText}, "\n")
	signature := ed25519.Sign(n.privateKey, []byte(message))
	request.Header.Set("X-Boxnet-ID", n.ID)
	request.Header.Set("X-Boxnet-Key", base64.RawURLEncoding.EncodeToString(n.publicKey))
	request.Header.Set("X-Boxnet-Member", n.membershipProof(n.publicKey))
	request.Header.Set("X-Boxnet-Time", timestamp)
	request.Header.Set("X-Boxnet-Nonce", nonceText)
	request.Header.Set("X-Boxnet-Signature", base64.RawURLEncoding.EncodeToString(signature))
	return nil
}

func (n *Node) verifyPeerRequest(request *http.Request) bool {
	publicKey, err := base64.RawURLEncoding.DecodeString(request.Header.Get("X-Boxnet-Key"))
	if err != nil || len(publicKey) != ed25519.PublicKeySize || nodeID(publicKey) != request.Header.Get("X-Boxnet-ID") || !n.isMember(publicKey, request.Header.Get("X-Boxnet-Member")) {
		return false
	}
	timestamp := request.Header.Get("X-Boxnet-Time")
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || time.Since(time.Unix(seconds, 0)).Abs() > 90*time.Second {
		return false
	}
	nonce := request.Header.Get("X-Boxnet-Nonce")
	if len(nonce) < 16 || !n.acceptNonce(request.Header.Get("X-Boxnet-ID")+":"+nonce) {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(request.Header.Get("X-Boxnet-Signature"))
	if err != nil {
		return false
	}
	message := strings.Join([]string{request.Method, request.URL.EscapedPath(), timestamp, nonce}, "\n")
	return ed25519.Verify(publicKey, []byte(message), signature)
}

func (n *Node) acceptNonce(nonce string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := time.Now()
	for value, seenAt := range n.seenNonces {
		if now.Sub(seenAt) > 2*time.Minute {
			delete(n.seenNonces, value)
		}
	}
	if _, exists := n.seenNonces[nonce]; exists {
		return false
	}
	n.seenNonces[nonce] = now
	return true
}

func (n *Node) verifyAdmin(request *http.Request) bool {
	provided := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
	if len(provided) != len(n.adminToken) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(n.adminToken)) == 1
}

func validatePeerURL(address string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(address, "/"))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("peer address must be a plain http(s) origin")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (n *Node) signedAnnouncement() announcement {
	a := announcement{Protocol: "boxnet/2", ID: n.ID, Name: n.Name, Address: n.Address, PublicKey: base64.RawURLEncoding.EncodeToString(n.publicKey), Membership: n.membershipProof(n.publicKey), Timestamp: time.Now().Unix()}
	a.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(n.privateKey, []byte(announcementMessage(a))))
	return a
}

func (n *Node) verifyAnnouncement(a announcement, source net.IP) (string, bool) {
	if a.Protocol != "boxnet/2" || time.Since(time.Unix(a.Timestamp, 0)).Abs() > 90*time.Second {
		return "", false
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(a.PublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize || nodeID(publicKey) != a.ID || !n.isMember(publicKey, a.Membership) {
		return "", false
	}
	signature, err := base64.RawURLEncoding.DecodeString(a.Signature)
	if err != nil || !ed25519.Verify(publicKey, []byte(announcementMessage(a)), signature) {
		return "", false
	}
	address, err := validatePeerURL(a.Address)
	if err != nil {
		return "", false
	}
	parsed, _ := url.Parse(address)
	advertisedIP := net.ParseIP(parsed.Hostname())
	if advertisedIP == nil || (!advertisedIP.IsLoopback() && !advertisedIP.Equal(source)) {
		return "", false
	}
	return address, true
}

func announcementMessage(a announcement) string {
	return strings.Join([]string{a.Protocol, a.ID, a.Name, a.Address, a.PublicKey, a.Membership, strconv.FormatInt(a.Timestamp, 10)}, "\n")
}

func (n *Node) signedManifest() Manifest {
	manifest := Manifest{NodeID: n.ID, Name: n.Name, Files: n.Store.List(), PublicKey: base64.RawURLEncoding.EncodeToString(n.publicKey), Membership: n.membershipProof(n.publicKey), Timestamp: time.Now().Unix()}
	manifest.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(n.privateKey, manifestMessage(manifest)))
	return manifest
}

func (n *Node) verifyManifest(manifest Manifest) bool {
	if time.Since(time.Unix(manifest.Timestamp, 0)).Abs() > 90*time.Second {
		return false
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(manifest.PublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize || nodeID(publicKey) != manifest.NodeID || !n.isMember(publicKey, manifest.Membership) {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(manifest.Signature)
	return err == nil && ed25519.Verify(publicKey, manifestMessage(manifest), signature)
}

func manifestMessage(manifest Manifest) []byte {
	unsigned := struct {
		NodeID     string       `json:"nodeId"`
		Name       string       `json:"name"`
		Files      []FileRecord `json:"files"`
		PublicKey  string       `json:"publicKey"`
		Membership string       `json:"membership"`
		Timestamp  int64        `json:"timestamp"`
	}{manifest.NodeID, manifest.Name, manifest.Files, manifest.PublicKey, manifest.Membership, manifest.Timestamp}
	encoded, _ := json.Marshal(unsigned)
	return encoded
}
