package main

import (
	"context"
	"encoding/json"
	"net"
	"time"
)

const multicastAddress = "239.255.67.89:9797"

func (n *Node) RunDiscovery(ctx context.Context) {
	group, err := net.ResolveUDPAddr("udp4", multicastAddress)
	if err != nil {
		n.log("warning", "LAN discovery unavailable: "+err.Error())
		return
	}
	conn, err := net.ListenMulticastUDP("udp4", nil, group)
	if err != nil {
		n.log("warning", "LAN discovery unavailable; seed peers still work")
		return
	}
	defer conn.Close()
	_ = conn.SetReadBuffer(64 * 1024)
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	go n.announceLoop(ctx, group)
	buf := make([]byte, 4096)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		count, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		var a announcement
		if json.Unmarshal(buf[:count], &a) == nil && a.Protocol == "boxnet/1" && a.ID != n.ID {
			n.rememberPeer(Peer{ID: a.ID, Name: a.Name, Address: a.Address, LastSeen: time.Now().UTC(), Online: true})
		}
	}
}

func (n *Node) announceLoop(ctx context.Context, address *net.UDPAddr) {
	conn, err := net.DialUDP("udp4", nil, address)
	if err != nil {
		return
	}
	defer conn.Close()
	payload, _ := json.Marshal(announcement{Protocol: "boxnet/1", ID: n.ID, Name: n.Name, Address: n.Address})
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		_, _ = conn.Write(payload)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
