package agent

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildArgsForCodexIncludesOutputFile(t *testing.T) {
	got := buildArgs(Codex, "hello", "/tmp/last-message.txt")
	want := []string{
		"exec",
		"hello",
		"--skip-git-repo-check",
		"--color",
		"never",
		"--output-last-message",
		"/tmp/last-message.txt",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected codex args: got %v want %v", got, want)
	}
}

func TestSelectOutputPrefersCodexOutputFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "last-message.txt")
	if err := os.WriteFile(path, []byte("  actual assistant reply  \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := selectOutput(Codex, "metadata on stdout", path)
	if got != "actual assistant reply" {
		t.Fatalf("expected output file content, got %q", got)
	}
}

func TestSelectOutputFallsBackToStdout(t *testing.T) {
	got := selectOutput(Codex, "stdout fallback", filepath.Join(t.TempDir(), "missing.txt"))
	if got != "stdout fallback" {
		t.Fatalf("expected stdout fallback, got %q", got)
	}

	got = selectOutput(Claude, "plain stdout", "")
	if got != "plain stdout" {
		t.Fatalf("expected non-codex stdout, got %q", got)
	}
}

func TestSelectOutputStripsCodexMetadataFromStdoutFallback(t *testing.T) {
	stdout := strings.TrimSpace(`
approval: never
sandbox: workspace-write
session id: 019e0baa

user
Latest user message:
Hi, who are you?
codex
I'm Codex, your coding assistant in this workspace.

Earlier I answered like "Claude," but in this session I'm Codex.
tokens used
10,230
`)

	got := selectOutput(Codex, stdout, filepath.Join(t.TempDir(), "missing.txt"))
	want := "I'm Codex, your coding assistant in this workspace.\n\nEarlier I answered like \"Claude,\" but in this session I'm Codex."
	if got != want {
		t.Fatalf("expected sanitized codex output %q, got %q", want, got)
	}
}

func TestSelectOutputSanitizesCodexOutputFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "last-message.txt")
	content := strings.TrimSpace(`
codex
Actual reply only.
tokens used
1,234
`)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := selectOutput(Codex, "metadata on stdout", path)
	if got != "Actual reply only." {
		t.Fatalf("expected sanitized output file content, got %q", got)
	}
}
