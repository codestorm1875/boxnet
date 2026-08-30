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
	advertise := flag.String("advertise", "http://localhost:8080", "URL other nodes use to reach this node")
	data := flag.String("data", "./box-data", "persistent data directory")
	replicas := flag.Int("replicas", 2, "desired number of copies across the mesh")
	peerList := flag.String("peers", "", "comma-separated seed peer URLs")
	ui := flag.String("ui", "web", "interface mode: web, browser, tui, or none")
	flag.Parse()

	var seeds []string
	for _, peer := range strings.Split(*peerList, ",") {
		if peer = strings.TrimSpace(peer); peer != "" {
			seeds = append(seeds, peer)
		}
	}
	node, err := NewNode(*name, *advertise, *data, *replicas, seeds)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go node.RunDiscovery(ctx)
	go node.RunSync(ctx)

	server := &http.Server{Addr: *listen, Handler: node.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	serverErrors := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = server.Shutdown(shutdownCtx)
	}()
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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
