package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTUIFileWorkflow(t *testing.T) {
	node, err := NewNode("Terminal Node", "http://localhost:8080", t.TempDir(), 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "field guide.txt")
	if err := os.WriteFile(source, []byte("offline knowledge"), 0o644); err != nil {
		t.Fatal(err)
	}
	record, added, err := publishLocal(node, source)
	if err != nil || !added || record.Name != "field guide.txt" {
		t.Fatalf("publishLocal() record=%#v added=%v err=%v", record, added, err)
	}
	destination := filepath.Join(t.TempDir(), "saved.txt")
	if err := exportLocal(node, 0, destination); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != "offline knowledge" {
		t.Fatalf("exported content=%q err=%v", content, err)
	}
	if err := exportLocal(node, 0, destination); err == nil {
		t.Fatal("export should not overwrite an existing file")
	}
}

func TestTUIRendersAndQuits(t *testing.T) {
	node, err := NewNode("Terminal Node", "http://localhost:8080", t.TempDir(), 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var output bytes.Buffer
	if err := RunTUI(ctx, node, cancel, strings.NewReader("help\nq\n"), &output); err != nil {
		t.Fatal(err)
	}
	view := output.String()
	if !strings.Contains(view, "BOXNET") || !strings.Contains(view, "Commands: add PATH") || !strings.Contains(view, "Boxnet stopped") {
		t.Fatalf("terminal output missing expected content: %q", view)
	}
}

func TestTUIHelpers(t *testing.T) {
	if got := humanBytes(1536); got != "1.5 KB" {
		t.Fatalf("humanBytes()=%q", got)
	}
	if got := truncate("abcdefgh", 5); got != "abcd…" {
		t.Fatalf("truncate()=%q", got)
	}
}

func TestTUICommandsAreTolerant(t *testing.T) {
	node, err := NewNode("Terminal Node", "http://localhost:8080", t.TempDir(), 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"help", "HELP", "?", "boxnet> help"} {
		message, quit := runTUICommand(context.Background(), node, command)
		if quit || !strings.Contains(message, "Commands: add PATH") {
			t.Fatalf("command %q returned message=%q quit=%v", command, message, quit)
		}
	}
}
