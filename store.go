package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Store struct {
	root  string
	mu    sync.RWMutex
	files map[string]FileRecord
}

func OpenStore(root string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(root, "blobs"), 0o755); err != nil {
		return nil, err
	}
	s := &Store{root: root, files: make(map[string]FileRecord)}
	b, err := os.ReadFile(filepath.Join(root, "index.json"))
	if err == nil {
		if err := json.Unmarshal(b, &s.files); err != nil {
			return nil, fmt.Errorf("read index: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return s, nil
}

func (s *Store) Put(r io.Reader, name, mediaType, originID string) (FileRecord, bool, error) {
	tmp, err := os.CreateTemp(filepath.Join(s.root, "blobs"), ".incoming-*")
	if err != nil {
		return FileRecord{}, false, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, h), r)
	closeErr := tmp.Close()
	if err != nil {
		return FileRecord{}, false, err
	}
	if closeErr != nil {
		return FileRecord{}, false, closeErr
	}
	hash := hex.EncodeToString(h.Sum(nil))
	record := FileRecord{Hash: hash, Name: name, Size: size, MediaType: mediaType, CreatedAt: time.Now().UTC(), OriginID: originID}
	return s.commitTemp(tmpName, record)
}

func (s *Store) PutReplica(r io.Reader, expected FileRecord) (FileRecord, bool, error) {
	record, added, err := s.Put(r, expected.Name, expected.MediaType, expected.OriginID)
	if err != nil {
		return FileRecord{}, false, err
	}
	if record.Hash != expected.Hash {
		if added {
			s.Remove(record.Hash)
		}
		return FileRecord{}, false, fmt.Errorf("hash mismatch: expected %s, got %s", expected.Hash, record.Hash)
	}
	if added {
		s.mu.Lock()
		record.CreatedAt = expected.CreatedAt
		s.files[record.Hash] = record
		err = s.saveLocked()
		s.mu.Unlock()
	}
	return record, added, err
}

func (s *Store) commitTemp(tmpName string, record FileRecord) (FileRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.files[record.Hash]; ok {
		return existing, false, nil
	}
	dest := s.BlobPath(record.Hash)
	if err := os.Rename(tmpName, dest); err != nil {
		return FileRecord{}, false, err
	}
	s.files[record.Hash] = record
	if err := s.saveLocked(); err != nil {
		delete(s.files, record.Hash)
		_ = os.Remove(dest)
		return FileRecord{}, false, err
	}
	return record, true, nil
}

func (s *Store) Remove(hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.files, hash)
	_ = os.Remove(s.BlobPath(hash))
	_ = s.saveLocked()
}

func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.files, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.root, "index.json.tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.root, "index.json"))
}

func (s *Store) BlobPath(hash string) string { return filepath.Join(s.root, "blobs", hash) }

func (s *Store) Get(hash string) (FileRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.files[hash]
	return r, ok
}

func (s *Store) List() []FileRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]FileRecord, 0, len(s.files))
	for _, f := range s.files {
		result = append(result, f)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result
}

func (s *Store) Stats() (int, int64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var bytes int64
	for _, f := range s.files {
		bytes += f.Size
	}
	return len(s.files), bytes
}
