package main

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

//go:embed web/*
var webFiles embed.FS

var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (n *Node) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", n.adminOnly(func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, n.Status()) }))
	mux.HandleFunc("GET /api/files", n.adminOnly(func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, n.FileViews()) }))
	mux.HandleFunc("GET /api/peers", n.adminOnly(func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, n.Peers()) }))
	mux.HandleFunc("GET /api/events", n.adminOnly(func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, n.Events()) }))
	mux.HandleFunc("GET /api/security", n.adminOnly(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"nodeId": n.ID, "invite": n.Invite(), "signedProtocol": true, "maxUpload": n.MaxUpload, "maxStorage": n.MaxStorage})
	}))
	mux.HandleFunc("GET /api/manifest", n.peerOnly(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, n.signedManifest())
	}))
	mux.HandleFunc("POST /api/files", n.adminOnly(n.uploadFile))
	mux.HandleFunc("GET /api/blobs/{hash}", n.peerOnly(n.serveBlob(false)))
	mux.HandleFunc("GET /api/files/{hash}/download", n.adminOnly(n.serveBlob(true)))
	mux.HandleFunc("POST /api/sync", n.adminOnly(func(w http.ResponseWriter, _ *http.Request) {
		go n.syncAll(context.Background())
		writeJSONStatus(w, http.StatusAccepted, map[string]string{"status": "sync started"})
	}))
	assets, _ := fs.Sub(webFiles, "web")
	mux.Handle("/", http.FileServer(http.FS(assets)))
	return securityHeaders(mux)
}

func (n *Node) uploadFile(w http.ResponseWriter, r *http.Request) {
	_, used := n.Store.Stats()
	remaining := n.MaxStorage - used
	limit := min(n.MaxUpload, remaining)
	if limit <= 0 {
		writeError(w, http.StatusInsufficientStorage, "Storage quota reached")
		return
	}
	if !n.acquireTransfer(w) {
		return
	}
	defer n.releaseTransfer()
	r.Body = http.MaxBytesReader(w, r.Body, limit+(1<<20))
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid upload or file exceeds the configured limit")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Choose a file to upload")
		return
	}
	defer file.Close()
	name := filepath.Base(header.Filename)
	if len(name) == 0 || len(name) > 255 {
		writeError(w, http.StatusBadRequest, "Filename must be between 1 and 255 bytes")
		return
	}
	buffered := bufio.NewReader(file)
	prefix, _ := buffered.Peek(512)
	mediaType := http.DetectContentType(prefix)
	record, added, err := n.Store.Put(io.LimitReader(buffered, limit+1), name, mediaType, n.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not store the file")
		return
	}
	if record.Size > limit {
		if added {
			n.Store.Remove(record.Hash)
		}
		writeError(w, http.StatusRequestEntityTooLarge, "File exceeds the available upload quota")
		return
	}
	if _, total := n.Store.Stats(); added && total > n.MaxStorage {
		n.Store.Remove(record.Hash)
		writeError(w, http.StatusInsufficientStorage, "Storage quota reached during upload")
		return
	}
	if added {
		n.log("file", fmt.Sprintf("Published %s", record.Name))
	}
	writeJSONStatus(w, http.StatusCreated, map[string]any{"file": record, "added": added})
}

func (n *Node) serveBlob(download bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !n.acquireTransfer(w) {
			return
		}
		defer n.releaseTransfer()
		hash := r.PathValue("hash")
		if !hashPattern.MatchString(hash) {
			writeError(w, http.StatusBadRequest, "Invalid content hash")
			return
		}
		record, ok := n.Store.Get(hash)
		if !ok {
			writeError(w, http.StatusNotFound, "File not found on this node")
			return
		}
		if download {
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", strings.ReplaceAll(record.Name, "\"", "")))
		}
		if download {
			w.Header().Set("Content-Type", record.MediaType)
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", "attachment")
		}
		w.Header().Set("ETag", `"`+record.Hash+`"`)
		http.ServeFile(w, r, n.Store.BlobPath(hash))
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (n *Node) adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !n.verifyAdmin(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="boxnet-dashboard"`)
			writeError(w, http.StatusUnauthorized, "Dashboard token required")
			return
		}
		next(w, r)
	}
}

func (n *Node) peerOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !n.verifyPeerRequest(r) {
			writeError(w, http.StatusUnauthorized, "Signed mesh membership required")
			return
		}
		next(w, r)
	}
}

func (n *Node) acquireTransfer(w http.ResponseWriter) bool {
	select {
	case n.transfers <- struct{}{}:
		return true
	default:
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusTooManyRequests, "Too many concurrent transfers")
		return false
	}
}

func (n *Node) releaseTransfer() { <-n.transfers }

func writeJSON(w http.ResponseWriter, value any) { writeJSONStatus(w, http.StatusOK, value) }

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSONStatus(w, status, map[string]string{"error": message, "timestamp": time.Now().UTC().Format(time.RFC3339)})
}
