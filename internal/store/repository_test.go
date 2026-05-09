package store

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"agentswitcher/internal/agent"
)

func TestRepositoryReplaceStandardsAndListStandards(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	session, err := repo.CreateSession(ctx, agent.Codex)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(2 * time.Millisecond)
	if err := repo.ReplaceStandards(ctx, session.ID, []string{"/tmp/b.md", "/tmp/a.md"}); err != nil {
		t.Fatal(err)
	}

	got, err := repo.ListStandards(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/tmp/a.md", "/tmp/b.md"}
	if paths := standardPaths(got); !reflect.DeepEqual(paths, want) {
		t.Fatalf("expected standards %v, got %v", want, paths)
	}

	updated, err := repo.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.UpdatedAt.After(session.UpdatedAt) {
		t.Fatalf("expected standards replacement to update session timestamp")
	}

	if err := repo.ReplaceStandards(ctx, session.ID, []string{"/tmp/c.md"}); err != nil {
		t.Fatal(err)
	}
	got, err = repo.ListStandards(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"/tmp/c.md"}
	if paths := standardPaths(got); !reflect.DeepEqual(paths, want) {
		t.Fatalf("expected replacement standards %v, got %v", want, paths)
	}
}

func TestRepositoryGetContextSnapshotReturnsRecentMessagesInAscendingOrder(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	session, err := repo.CreateSession(ctx, agent.Codex)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceStandards(ctx, session.ID, []string{"/tmp/rules.md"}); err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= 13; i++ {
		if _, err := repo.AddExchange(ctx, session.ID, fmt.Sprintf("user-%02d", i), fmt.Sprintf("assistant-%02d", i)); err != nil {
			t.Fatal(err)
		}
	}

	snapshot, err := repo.GetContextSnapshot(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(snapshot.Standards) != 1 || snapshot.Standards[0].Path != "/tmp/rules.md" {
		t.Fatalf("unexpected standards in snapshot: %#v", snapshot.Standards)
	}
	if len(snapshot.RecentMessages) != 24 {
		t.Fatalf("expected 24 recent messages, got %d", len(snapshot.RecentMessages))
	}
	if snapshot.RecentMessages[0].Content != "user-02" {
		t.Fatalf("expected oldest retained message to be user-02, got %q", snapshot.RecentMessages[0].Content)
	}
	last := snapshot.RecentMessages[len(snapshot.RecentMessages)-1]
	if last.Content != "assistant-13" {
		t.Fatalf("expected newest retained message to be assistant-13, got %q", last.Content)
	}
}

func TestRepositoryGetMessagesForCompactionExcludesCompactedTurns(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	session, err := repo.CreateSession(ctx, agent.Codex)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		if _, err := repo.AddExchange(ctx, session.ID, fmt.Sprintf("prompt-%d", i), fmt.Sprintf("reply-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.SaveCompaction(ctx, session.ID, "summary", 2); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetMessagesForCompaction(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 uncompacted messages, got %d", len(got))
	}
	if got[0].Content != "prompt-3" || got[1].Content != "reply-3" {
		t.Fatalf("unexpected uncompacted messages: %#v", got)
	}
}

func TestRepositoryAddExchangeAndCompactionState(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	session, err := repo.CreateSession(ctx, agent.Claude)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := repo.AddExchange(ctx, session.ID, "   first prompt   ", " first reply ")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "first prompt" {
		t.Fatalf("expected first prompt to become title, got %q", updated.Title)
	}
	if updated.UserPromptCount != 1 {
		t.Fatalf("expected prompt count 1, got %d", updated.UserPromptCount)
	}
	if repo.NeedCompaction(updated) {
		t.Fatalf("did not expect compaction after first prompt")
	}

	for i := 2; i <= 12; i++ {
		updated, err = repo.AddExchange(ctx, session.ID, fmt.Sprintf("prompt %d", i), fmt.Sprintf("reply %d", i))
		if err != nil {
			t.Fatal(err)
		}
	}
	if !repo.NeedCompaction(updated) {
		t.Fatalf("expected compaction to be needed after 12 prompts")
	}

	compacted, err := repo.SaveCompaction(ctx, session.ID, "  compacted summary  ", 12)
	if err != nil {
		t.Fatal(err)
	}
	if compacted.Summary != "compacted summary" {
		t.Fatalf("expected trimmed summary, got %q", compacted.Summary)
	}
	if repo.NeedCompaction(compacted) {
		t.Fatalf("did not expect compaction immediately after saving compaction state")
	}
}

func TestRepositoryUpdateSessionAgent(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	session, err := repo.CreateSession(ctx, agent.Codex)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(2 * time.Millisecond)
	updated, err := repo.UpdateSessionAgent(ctx, session.ID, agent.Claude)
	if err != nil {
		t.Fatal(err)
	}

	if updated.ID != session.ID {
		t.Fatalf("expected session id %q to be preserved, got %q", session.ID, updated.ID)
	}
	if updated.Agent != agent.Claude {
		t.Fatalf("expected agent %q, got %q", agent.Claude, updated.Agent)
	}
	if !updated.UpdatedAt.After(session.UpdatedAt) {
		t.Fatalf("expected session update timestamp to move forward")
	}
}

func newTestRepository(t *testing.T) *Repository {
	t.Helper()

	repo, err := NewRepository(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = repo.Close()
	})
	return repo
}

func standardPaths(standards []Standard) []string {
	paths := make([]string, 0, len(standards))
	for _, standard := range standards {
		paths = append(paths, standard.Path)
	}
	return paths
}
