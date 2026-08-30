package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestStorePersistsContentAddressedFile(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	record, added, err := store.Put(strings.NewReader("hello mesh"), "hello.txt", "text/plain", "node-a")
	if err != nil || !added {
		t.Fatalf("Put() added=%v err=%v", added, err)
	}
	if record.Hash != "0d86592ccb0e25a961d1515f245609934ee506d3de06f2481ebd4eb8f224e248" {
		t.Fatalf("unexpected hash %q", record.Hash)
	}
	if _, err := os.Stat(store.BlobPath(record.Hash)); err != nil {
		t.Fatalf("blob was not stored: %v", err)
	}
	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reopened.Get(record.Hash); !ok || got.Name != "hello.txt" {
		t.Fatalf("record did not persist: %#v, %v", got, ok)
	}
}

func TestNodesReplicateAndRetainFileWhenSourceStops(t *testing.T) {
	source, err := NewNode("Source", "", t.TempDir(), 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := source.Store.Put(strings.NewReader("available offline"), "guide.txt", "text/plain", source.ID)
	if err != nil {
		t.Fatal(err)
	}
	replica, err := NewNode("Replica", "", t.TempDir(), 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	sourceHandler := source.Handler()
	replica.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		sourceHandler.ServeHTTP(recorder, request)
		return recorder.Result(), nil
	})
	replica.syncPeer(context.Background(), "http://source")

	if _, ok := replica.Store.Get(record.Hash); !ok {
		t.Fatal("replica did not receive file")
	}
	file, err := os.Open(replica.Store.BlobPath(record.Hash))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	body, _ := io.ReadAll(file)
	if string(body) != "available offline" {
		t.Fatalf("unexpected replicated content %q", body)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func TestReplicaRejectsCorruptContent(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	expected := FileRecord{Hash: strings.Repeat("a", 64), Name: "bad.txt", MediaType: "text/plain"}
	if _, _, err := store.PutReplica(strings.NewReader("not the expected content"), expected); err == nil {
		t.Fatal("expected hash mismatch")
	}
	if count, _ := store.Stats(); count != 0 {
		t.Fatalf("corrupt file remained in store; count=%d", count)
	}
}
