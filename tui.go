package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	ansiClear = "\033[2J\033[H"
	ansiBold  = "\033[1m"
	ansiDim   = "\033[2m"
	ansiGreen = "\033[32m"
	ansiAmber = "\033[33m"
	ansiReset = "\033[0m"
)

func RunTUI(ctx context.Context, node *Node, stop context.CancelFunc, input io.Reader, output io.Writer) error {
	commands := make(chan string)
	scanErrors := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(input)
		for scanner.Scan() {
			commands <- scanner.Text()
		}
		scanErrors <- scanner.Err()
		close(commands)
	}()
	message := "Node ready. Type help to see commands."
	for {
		renderTUI(output, node, message)
		select {
		case <-ctx.Done():
			fmt.Fprint(output, ansiReset, "\n")
			return nil
		case line, ok := <-commands:
			if !ok {
				stop()
				if err := <-scanErrors; err != nil {
					return fmt.Errorf("read command: %w", err)
				}
				return nil
			}
			var quit bool
			message, quit = runTUICommand(ctx, node, strings.TrimSpace(line))
			if quit {
				stop()
				fmt.Fprint(output, ansiClear, "Boxnet stopped.\n")
				return nil
			}
		}
	}
}

func renderTUI(w io.Writer, node *Node, message string) {
	status := node.Status()
	files := node.FileViews()
	peers := node.Peers()
	events := node.Events()
	color := ansiGreen
	if status.Health != "healthy" {
		color = ansiAmber
	}
	fmt.Fprint(w, ansiClear)
	fmt.Fprintf(w, "%sBOXNET%s  /  %s%s%s\n", ansiBold, ansiReset, ansiBold, status.Name, ansiReset)
	fmt.Fprintf(w, "%s%s● %s%s  %s  %s%s\n\n", color, ansiBold, strings.ToUpper(status.Health), ansiReset, status.Address, ansiDim, status.ID)
	fmt.Fprintf(w, "%sNETWORK%s\n", ansiBold, ansiReset)
	fmt.Fprintf(w, "  Files  %-8d Storage  %-10s Peers  %d/%d online  Coverage  %.0f%%\n\n", status.Files, humanBytes(status.Bytes), status.OnlinePeers, status.KnownPeers, status.Coverage)
	fmt.Fprintf(w, "%sFILES%s\n", ansiBold, ansiReset)
	if len(files) == 0 {
		fmt.Fprintln(w, "  No files published yet.")
	} else {
		fmt.Fprintln(w, "  #   NAME                              SIZE       COPIES  HASH")
		for i, file := range files {
			fmt.Fprintf(w, "  %-3d %-33s %-10s %d/%-5d %.12s\n", i+1, truncate(file.Name, 33), humanBytes(file.Size), file.Replicas, status.ReplicationFactor, file.Hash)
		}
	}
	fmt.Fprintf(w, "\n%sPEERS%s\n", ansiBold, ansiReset)
	if len(peers) == 0 {
		fmt.Fprintln(w, "  No peers discovered.")
	} else {
		for _, peer := range peers {
			state := "offline"
			if peer.Online {
				state = "online"
			}
			fmt.Fprintf(w, "  %-24s %-8s %s\n", truncate(peer.Name, 24), state, peer.Address)
		}
	}
	fmt.Fprintf(w, "\n%sRECENT ACTIVITY%s\n", ansiBold, ansiReset)
	for i, event := range events {
		if i == 4 {
			break
		}
		fmt.Fprintf(w, "  %-8s %s\n", relativeTime(event.At), event.Message)
	}
	fmt.Fprintf(w, "\n%s%s%s\n", ansiDim, message, ansiReset)
	fmt.Fprint(w, "boxnet> ")
}

func runTUICommand(ctx context.Context, node *Node, line string) (string, bool) {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(strings.ToLower(line), "boxnet>") {
		line = strings.TrimSpace(line[len("boxnet>"):])
	}
	command := strings.ToLower(line)
	switch {
	case command == "", command == "refresh", command == "r":
		return "State refreshed.", false
	case command == "help", command == "h", command == "?":
		return "Commands: add PATH · save NUMBER DESTINATION · sync · invite · token · refresh · quit", false
	case command == "sync", command == "s":
		go node.syncAll(ctx)
		return "Peer synchronization started.", false
	case command == "invite":
		return "Mesh invitation (keep secret): " + node.Invite(), false
	case command == "token":
		return "Dashboard token (keep secret): " + node.adminToken, false
	case command == "quit", command == "q", command == "exit":
		return "", true
	case strings.HasPrefix(command, "add "):
		path := strings.TrimSpace(line[4:])
		record, added, err := publishLocal(node, path)
		if err != nil {
			return "Could not add file: " + err.Error(), false
		}
		if !added {
			return record.Name + " is already stored on this node.", false
		}
		return "Published " + record.Name + ".", false
	case strings.HasPrefix(command, "save "):
		parts := strings.SplitN(strings.TrimSpace(line[5:]), " ", 2)
		if len(parts) != 2 {
			return "Usage: save NUMBER DESTINATION", false
		}
		index, err := strconv.Atoi(parts[0])
		if err != nil || index < 1 {
			return "File number must match the list above.", false
		}
		if err := exportLocal(node, index-1, strings.TrimSpace(parts[1])); err != nil {
			return "Could not save file: " + err.Error(), false
		}
		return "File saved to " + strings.TrimSpace(parts[1]) + ".", false
	default:
		return "Unknown command. Type help to see commands.", false
	}
}

func publishLocal(node *Node, path string) (FileRecord, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return FileRecord{}, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return FileRecord{}, false, err
	}
	_, used := node.Store.Stats()
	if info.Size() > node.MaxUpload || info.Size() > node.MaxStorage-used {
		return FileRecord{}, false, errors.New("file exceeds this node's storage policy")
	}
	mediaType := mime.TypeByExtension(filepath.Ext(path))
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	record, added, err := node.Store.Put(file, filepath.Base(path), mediaType, node.ID)
	if err == nil && added {
		if _, total := node.Store.Stats(); total > node.MaxStorage {
			node.Store.Remove(record.Hash)
			return FileRecord{}, false, errors.New("storage quota reached during publish")
		}
	}
	if err == nil && added {
		node.log("file", fmt.Sprintf("Published %s from terminal", record.Name))
	}
	return record, added, err
}

func exportLocal(node *Node, index int, destination string) error {
	files := node.Store.List()
	if index < 0 || index >= len(files) {
		return fmt.Errorf("file number is out of range")
	}
	source, err := os.Open(node.Store.BlobPath(files[index].Hash))
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil {
		_ = os.Remove(destination)
		return copyErr
	}
	return closeErr
}

func humanBytes(value int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	size := float64(value)
	unit := 0
	for size >= 1024 && unit < len(units)-1 {
		size /= 1024
		unit++
	}
	if unit == 0 || size >= 10 {
		return fmt.Sprintf("%.0f %s", size, units[unit])
	}
	return fmt.Sprintf("%.1f %s", size, units[unit])
}

func truncate(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	return string(runes[:width-1]) + "…"
}

func relativeTime(at time.Time) string {
	d := time.Since(at)
	if d < time.Minute {
		return "now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh ago", int(d.Hours()))
}
