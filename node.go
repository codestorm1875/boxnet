package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Node struct {
	ID          string
	Name        string
	Address     string
	Store       *Store
	Replication int
	startedAt   time.Time
	client      *http.Client
	privateKey  ed25519.PrivateKey
	publicKey   ed25519.PublicKey
	meshKey     []byte
	adminToken  string
	newMesh     bool
	MaxUpload   int64
	MaxStorage  int64
	mu          sync.RWMutex
	peers       map[string]Peer
	peerFiles   map[string]map[string]bool
	events      []Event
	seeds       []string
	seenNonces  map[string]time.Time
	transfers   chan struct{}
}

func NewNode(name, address, dataDir string, replication int, seeds []string, invite ...string) (*Node, error) {
	store, err := OpenStore(dataDir)
	if err != nil {
		return nil, err
	}
	joinInvite := ""
	if len(invite) > 0 {
		joinInvite = invite[0]
	}
	security, err := loadSecurity(dataDir, joinInvite)
	if err != nil {
		return nil, err
	}
	if replication < 1 {
		replication = 1
	}
	validatedSeeds := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		validated, validateErr := validatePeerURL(seed)
		if validateErr != nil {
			return nil, fmt.Errorf("seed %q: %w", seed, validateErr)
		}
		validatedSeeds = append(validatedSeeds, validated)
	}
	n := &Node{ID: nodeID(security.publicKey), Name: name, Address: strings.TrimRight(address, "/"), Store: store, Replication: replication, startedAt: time.Now().UTC(), client: &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, privateKey: security.privateKey, publicKey: security.publicKey, meshKey: security.meshKey, adminToken: security.adminToken, newMesh: security.newMesh, MaxUpload: 1 << 30, MaxStorage: 10 << 30, peers: make(map[string]Peer), peerFiles: make(map[string]map[string]bool), seeds: validatedSeeds, seenNonces: make(map[string]time.Time), transfers: make(chan struct{}, 4)}
	n.log("node", "Node started and local storage is ready")
	return n, nil
}

func (n *Node) log(kind, message string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append([]Event{{At: time.Now().UTC(), Kind: kind, Message: message}}, n.events...)
	if len(n.events) > 100 {
		n.events = n.events[:100]
	}
}

func (n *Node) Events() []Event {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return append([]Event(nil), n.events...)
}

func (n *Node) Peers() []Peer {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := time.Now()
	result := make([]Peer, 0, len(n.peers))
	for id, p := range n.peers {
		p.Online = now.Sub(p.LastSeen) < 12*time.Second
		n.peers[id] = p
		result = append(result, p)
	}
	return result
}

func (n *Node) rememberPeer(p Peer) {
	if p.ID == "" || p.ID == n.ID || p.Address == "" {
		return
	}
	n.mu.Lock()
	old, existed := n.peers[p.ID]
	p.Online = true
	n.peers[p.ID] = p
	n.mu.Unlock()
	if !existed || !old.Online {
		n.log("peer", fmt.Sprintf("Connected to %s", p.Name))
	}
}

func (n *Node) RunSync(ctx context.Context) {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	n.syncAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.syncAll(ctx)
		}
	}
}

func (n *Node) syncAll(ctx context.Context) {
	addresses := append([]string(nil), n.seeds...)
	for _, p := range n.Peers() {
		if p.Online {
			addresses = append(addresses, p.Address)
		}
	}
	seen := map[string]bool{}
	for _, address := range addresses {
		address = strings.TrimRight(address, "/")
		if address == "" || address == n.Address || seen[address] {
			continue
		}
		seen[address] = true
		n.syncPeer(ctx, address)
	}
}

func (n *Node) syncPeer(ctx context.Context, address string) {
	validated, err := validatePeerURL(address)
	if err != nil {
		return
	}
	address = validated
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, address+"/api/manifest", nil)
	if err := n.signPeerRequest(req); err != nil {
		return
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var manifest Manifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&manifest); err != nil || manifest.NodeID == n.ID || !n.verifyManifest(manifest) {
		return
	}
	n.rememberPeer(Peer{ID: manifest.NodeID, Name: manifest.Name, Address: address, LastSeen: time.Now().UTC(), Files: len(manifest.Files), Online: true})
	n.mu.Lock()
	n.peerFiles[manifest.NodeID] = make(map[string]bool, len(manifest.Files))
	for _, file := range manifest.Files {
		n.peerFiles[manifest.NodeID][file.Hash] = true
	}
	n.mu.Unlock()
	for _, file := range manifest.Files {
		if _, exists := n.Store.Get(file.Hash); exists {
			continue
		}
		n.pullFile(ctx, address, file)
	}
}

func (n *Node) pullFile(ctx context.Context, address string, file FileRecord) {
	_, used := n.Store.Stats()
	limit := min(n.MaxUpload, n.MaxStorage-used)
	if file.Size < 0 || file.Size > limit || limit <= 0 {
		n.log("warning", fmt.Sprintf("Skipped %s because it exceeds this node's storage policy", file.Name))
		return
	}
	select {
	case n.transfers <- struct{}{}:
		defer func() { <-n.transfers }()
	default:
		return
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, address+"/api/blobs/"+file.Hash, nil)
	if err := n.signPeerRequest(req); err != nil {
		return
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	if record, added, err := n.Store.PutReplica(io.LimitReader(resp.Body, limit+1), file); err == nil && added {
		if _, total := n.Store.Stats(); total > n.MaxStorage {
			n.Store.Remove(record.Hash)
			n.log("warning", fmt.Sprintf("Discarded %s because the storage quota was reached", file.Name))
			return
		}
		n.log("sync", fmt.Sprintf("Replicated %s from %s", file.Name, address))
	}
}

func (n *Node) Status() Status {
	files, bytes := n.Store.Stats()
	peers := n.Peers()
	online := 0
	for _, p := range peers {
		if p.Online {
			online++
		}
	}
	coverage := 100.0
	if files > 0 {
		var total float64
		for _, file := range n.FileViews() {
			total += float64(min(n.Replication, file.Replicas)) / float64(n.Replication)
		}
		coverage = total / float64(files) * 100
	}
	health := "healthy"
	if coverage < 100 {
		health = "degraded"
	}
	return Status{ID: n.ID, Name: n.Name, Address: n.Address, Files: files, Bytes: bytes, OnlinePeers: online, KnownPeers: len(peers), ReplicationFactor: n.Replication, StartedAt: n.startedAt.Format(time.RFC3339), Health: health, StoragePath: n.Store.root, Coverage: coverage}
}

func (n *Node) FileViews() []FileView {
	files := n.Store.List()
	peers := n.Peers()
	n.mu.RLock()
	defer n.mu.RUnlock()
	views := make([]FileView, 0, len(files))
	for _, file := range files {
		copies := 1
		for _, peer := range peers {
			if peer.Online && n.peerFiles[peer.ID][file.Hash] {
				copies++
			}
		}
		views = append(views, FileView{FileRecord: file, Replicas: copies})
	}
	return views
}
