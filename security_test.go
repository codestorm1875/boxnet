package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityAndMeshPersistSecurely(t *testing.T) {
	dir := t.TempDir()
	first, err := NewNode("First", "http://127.0.0.1:8080", dir, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewNode("Second Name", "http://127.0.0.1:8080", dir, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.Invite() != second.Invite() || first.adminToken != second.adminToken {
		t.Fatal("security identity did not persist")
	}
	for _, name := range []string{"identity.key", "mesh.key", "admin.token"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s permissions=%v", name, info.Mode().Perm())
		}
	}
}

func TestSignedDiscoveryAndManifestRejectTampering(t *testing.T) {
	node, err := NewNode("Signer", "http://127.0.0.1:8080", t.TempDir(), 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	member, err := NewNode("Verifier", "http://127.0.0.1:8081", t.TempDir(), 2, nil, node.Invite())
	if err != nil {
		t.Fatal(err)
	}
	announcement := node.signedAnnouncement()
	if _, valid := member.verifyAnnouncement(announcement, net.ParseIP("127.0.0.1")); !valid {
		t.Fatal("valid signed announcement was rejected")
	}
	announcement.Name = "Impostor"
	if _, valid := member.verifyAnnouncement(announcement, net.ParseIP("127.0.0.1")); valid {
		t.Fatal("tampered announcement was accepted")
	}
	manifest := node.signedManifest()
	if !member.verifyManifest(manifest) {
		t.Fatal("valid signed manifest was rejected")
	}
	manifest.Name = "Impostor"
	if member.verifyManifest(manifest) {
		t.Fatal("tampered manifest was accepted")
	}
}

func TestSignedPeerRequestAndReplayProtection(t *testing.T) {
	mesh, err := NewNode("Mesh", "http://127.0.0.1:8080", t.TempDir(), 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := NewNode("Peer", "http://127.0.0.1:8081", t.TempDir(), 2, nil, mesh.Invite())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://mesh/api/manifest", nil)
	if err := peer.signPeerRequest(request); err != nil {
		t.Fatal(err)
	}
	if !mesh.verifyPeerRequest(request) {
		t.Fatal("valid signed request was rejected")
	}
	if mesh.verifyPeerRequest(request) {
		t.Fatal("replayed request was accepted")
	}
}

func TestForeignMeshAndMissingAdminTokenAreRejected(t *testing.T) {
	node, err := NewNode("Protected", "http://127.0.0.1:8080", t.TempDir(), 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := NewNode("Foreign", "http://127.0.0.1:8081", t.TempDir(), 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	peerRequest := httptest.NewRequest(http.MethodGet, "/api/manifest", nil)
	_ = foreign.signPeerRequest(peerRequest)
	peerResponse := httptest.NewRecorder()
	node.Handler().ServeHTTP(peerResponse, peerRequest)
	if peerResponse.Code != http.StatusUnauthorized {
		t.Fatalf("foreign peer status=%d", peerResponse.Code)
	}

	adminRequest := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	adminResponse := httptest.NewRecorder()
	node.Handler().ServeHTTP(adminResponse, adminRequest)
	if adminResponse.Code != http.StatusUnauthorized {
		t.Fatalf("missing admin token status=%d", adminResponse.Code)
	}
	adminRequest = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	adminRequest.Header.Set("Authorization", "Bearer "+node.adminToken)
	adminResponse = httptest.NewRecorder()
	node.Handler().ServeHTTP(adminResponse, adminRequest)
	if adminResponse.Code != http.StatusOK {
		t.Fatalf("valid admin token status=%d", adminResponse.Code)
	}
}

func TestPeerURLValidation(t *testing.T) {
	invalid := []string{"file:///etc/passwd", "http://user:pass@host", "http://host/path", "http://host?next=x", "javascript:alert(1)"}
	for _, address := range invalid {
		if _, err := validatePeerURL(address); err == nil {
			t.Fatalf("accepted unsafe peer URL %q", address)
		}
	}
	if got, err := validatePeerURL("https://192.168.1.2:8443/"); err != nil || got != "https://192.168.1.2:8443" {
		t.Fatalf("valid peer URL got=%q err=%v", got, err)
	}
}
