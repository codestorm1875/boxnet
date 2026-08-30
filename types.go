package main

import "time"

type FileRecord struct {
	Hash      string    `json:"hash"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	MediaType string    `json:"mediaType"`
	CreatedAt time.Time `json:"createdAt"`
	OriginID  string    `json:"originId"`
}

type FileView struct {
	FileRecord
	Replicas int `json:"replicas"`
}

type Peer struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Address  string    `json:"address"`
	LastSeen time.Time `json:"lastSeen"`
	Files    int       `json:"files"`
	Online   bool      `json:"online"`
}

type Manifest struct {
	NodeID string       `json:"nodeId"`
	Name   string       `json:"name"`
	Files  []FileRecord `json:"files"`
}

type Event struct {
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"`
	Message string    `json:"message"`
}

type Status struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Address           string  `json:"address"`
	Files             int     `json:"files"`
	Bytes             int64   `json:"bytes"`
	OnlinePeers       int     `json:"onlinePeers"`
	KnownPeers        int     `json:"knownPeers"`
	ReplicationFactor int     `json:"replicationFactor"`
	StartedAt         string  `json:"startedAt"`
	Health            string  `json:"health"`
	StoragePath       string  `json:"storagePath"`
	Coverage          float64 `json:"coverage"`
}

type announcement struct {
	Protocol string `json:"protocol"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Address  string `json:"address"`
}
