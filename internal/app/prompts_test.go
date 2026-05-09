package app

import (
	"strings"
	"testing"
	"time"

	"agentswitcher/internal/agent"
	"agentswitcher/internal/store"
)

func TestBuildAgentPromptWithStandards(t *testing.T) {
	session := store.Session{
		Agent:   agent.Codex,
		Summary: "Keep using Go.",
	}
	recent := []store.Message{
		{
			Role:      "user",
			Content:   "Write tests.",
			CreatedAt: time.Date(2026, 5, 9, 1, 2, 3, 0, time.UTC),
		},
		{
			Role:      "assistant",
			Content:   "I will add unit tests.",
			CreatedAt: time.Date(2026, 5, 9, 1, 3, 4, 0, time.UTC),
		},
	}
	standards := []promptStandard{
		{
			Path:    "/tmp/STANDARDS.md",
			Content: "Always write table-driven tests.",
		},
	}

	got := buildAgentPromptWithStandards(session, standards, recent, "Add repo coverage")

	wantContains := []string{
		"You are continuing an existing codex conversation.",
		"External standards documents.",
		"Standard file: /tmp/STANDARDS.md",
		"Always write table-driven tests.",
		"Conversation summary:\nKeep using Go.",
		"[2026-05-09T01:02:03Z] USER:",
		"Write tests.",
		"[2026-05-09T01:03:04Z] ASSISTANT:",
		"I will add unit tests.",
		"Latest user message:\nAdd repo coverage",
	}

	for _, want := range wantContains {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q\nfull prompt:\n%s", want, got)
		}
	}
}

func TestBuildCompactionPromptWithStandardsExcludesStandardContent(t *testing.T) {
	session := store.Session{Summary: "Previous summary."}
	messages := []store.Message{
		{
			Role:      "user",
			Content:   "Need more tests.",
			CreatedAt: time.Date(2026, 5, 9, 2, 0, 0, 0, time.UTC),
		},
	}
	standards := []promptStandard{
		{
			Path:    "/tmp/testing.md",
			Content: "Never summarize this document.",
		},
	}

	got := buildCompactionPromptWithStandards(session, messages, standards)

	wantContains := []string{
		"Keep the summary under 600 words.",
		"Do not summarize or restate external standards documents.",
		"Existing summary:\nPrevious summary.",
		"Excluded external standards files:\n- /tmp/testing.md",
		"[2026-05-09T02:00:00Z] USER:",
		"Need more tests.",
	}
	for _, want := range wantContains {
		if !strings.Contains(got, want) {
			t.Fatalf("compaction prompt missing %q\nfull prompt:\n%s", want, got)
		}
	}

	if strings.Contains(got, "Never summarize this document.") {
		t.Fatalf("compaction prompt unexpectedly included standard content:\n%s", got)
	}
}

func TestDescribeAgentFallsBackToKind(t *testing.T) {
	if got := describeAgent(agent.Claude); got != "Claude" {
		t.Fatalf("expected known agent label, got %q", got)
	}

	if got := describeAgent(agent.Kind("custom")); got != "custom" {
		t.Fatalf("expected unknown agent kind fallback, got %q", got)
	}
}
