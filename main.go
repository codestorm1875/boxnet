package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
)

var execCommand = exec.Command

func main() {
	name := flag.String("name", "Home Node", "human-readable node name")
	listen := flag.String("listen", ":8080", "HTTP listen address")
	advertise := flag.String("advertise", "https://localhost:8080", "HTTPS URL other nodes use to reach this node")
	data := flag.String("data", "./box-data", "persistent data directory")
	replicas := flag.Int("replicas", 2, "desired number of copies across the mesh")
	peerList := flag.String("peers", "", "comma-separated seed peer URLs")
	ui := flag.String("ui", "web", "interface mode: web, browser, tui, or none")
	join := flag.String("join", "", "one-time mesh invitation (prefer -join-file to avoid shell history)")
	joinFile := flag.String("join-file", "", "path to a file containing a mesh invitation")
	showInvite := flag.Bool("show-invite", false, "print the mesh invitation on startup")
	maxUpload := flag.Int64("max-upload", 1<<30, "maximum bytes in one uploaded file")
	maxStorage := flag.Int64("max-storage", 10<<30, "maximum bytes stored on this node")
	flag.Parse()

	var seeds []string
	for _, peer := range strings.Split(*peerList, ",") {
		if peer = strings.TrimSpace(peer); peer != "" {
			seeds = append(seeds, peer)
		}
	}
	joinValue := strings.TrimSpace(*join)
	if *joinFile != "" {
		if joinValue != "" {
			log.Fatal("use only one of -join or -join-file")
		}
		value, readErr := os.ReadFile(*joinFile)
		if readErr != nil {
			log.Fatalf("read invitation: %v", readErr)
		}
		joinValue = strings.TrimSpace(string(value))
	}
	node, err := NewNode(*name, *advertise, *data, *replicas, seeds, joinValue)
	if err != nil {
		log.Fatal(err)
	}
	if *maxUpload <= 0 || *maxStorage <= 0 || *maxUpload > *maxStorage {
		log.Fatal("storage limits must be positive and max-upload cannot exceed max-storage")
	}
	node.MaxUpload = *maxUpload
	node.MaxStorage = *maxStorage
	if node.newMesh || *showInvite {
		fmt.Printf("Mesh invitation (keep secret): %s\n", node.Invite())
	}
	fmt.Printf("Dashboard token: %s\n", node.adminToken)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go node.RunDiscovery(ctx)
	go node.RunSync(ctx)

	server := &http.Server{Addr: *listen, Handler: node.Handler(), TLSConfig: node.serverTLSConfig(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	serverErrors := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = server.Shutdown(shutdownCtx)
	}()
	go func() {
		if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			serverErrors <- err
		}
	}()

	switch *ui {
	case "browser":
		fmt.Printf("Boxnet node %q is ready at %s\n", node.Name, node.Address)
		time.Sleep(100 * time.Millisecond)
		if err := openBrowser(node.Address); err != nil {
			log.Printf("could not open browser: %v", err)
		}
	case "tui":
		if err := RunTUI(ctx, node, cancel, os.Stdin, os.Stdout); err != nil {
			log.Printf("terminal interface: %v", err)
		}
		cancel()
		return
	case "web", "none":
		fmt.Printf("Boxnet node %q is ready at %s\n", node.Name, node.Address)
		fmt.Printf("Node ID: %s | data: %s\n", node.ID, *data)
	default:
		log.Fatalf("unknown -ui value %q (use web, browser, tui, or none)", *ui)
	}

	select {
	case <-ctx.Done():
	case err := <-serverErrors:
		log.Fatal(err)
	}
}

func openBrowser(address string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command = "open"
		args = []string{address}
	case "windows":
		command = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", address}
	default:
		command = "xdg-open"
		args = []string{address}
	}
	return execCommand(command, args...).Start()
}
