package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
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
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, n.Status()) })
	mux.HandleFunc("GET /api/files", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, n.FileViews()) })
	mux.HandleFunc("GET /api/peers", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, n.Peers()) })
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, n.Events()) })
	mux.HandleFunc("GET /api/manifest", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, Manifest{NodeID: n.ID, Name: n.Name, Files: n.Store.List()})
	})
	mux.HandleFunc("POST /api/files", n.uploadFile)
	mux.HandleFunc("GET /api/blobs/{hash}", n.serveBlob(false))
	mux.HandleFunc("GET /api/files/{hash}/download", n.serveBlob(true))
	mux.HandleFunc("POST /api/sync", func(w http.ResponseWriter, _ *http.Request) {
		go n.syncAll(context.Background())
		writeJSONStatus(w, http.StatusAccepted, map[string]string{"status": "sync started"})
	})
	assets, _ := fs.Sub(webFiles, "web")
	mux.Handle("/", http.FileServer(http.FS(assets)))
	return securityHeaders(mux)
}

func (n *Node) uploadFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<30)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid upload or file exceeds the 1 GB limit")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Choose a file to upload")
		return
	}
	defer file.Close()
	name := filepath.Base(header.Filename)
	mediaType := header.Header.Get("Content-Type")
	record, added, err := n.Store.Put(file, name, mediaType, n.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not store the file")
		return
	}
	if added {
		n.log("file", fmt.Sprintf("Published %s", record.Name))
	}
	writeJSONStatus(w, http.StatusCreated, map[string]any{"file": record, "added": added})
}

func (n *Node) serveBlob(download bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		w.Header().Set("Content-Type", record.MediaType)
		w.Header().Set("ETag", `"`+record.Hash+`"`)
		http.ServeFile(w, r, n.Store.BlobPath(hash))
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, value any) { writeJSONStatus(w, http.StatusOK, value) }

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSONStatus(w, status, map[string]string{"error": message, "timestamp": time.Now().UTC().Format(time.RFC3339)})
}
