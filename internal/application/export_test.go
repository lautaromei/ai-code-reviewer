package application

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/application/applicationtest"
	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

func newExportStore(t *testing.T) *fakeExportStore {
	t.Helper()
	ctx := context.Background()
	store := newFakeExportStore()
	applicationtest.NewRepositoryDirectorySeeder(t, store.repositories, ctx).Many(
		applicationtest.Repository.WithFullName("example-org/example-alpha").Bare(),
		applicationtest.Repository.WithFullName("example-org/example-beta").Bare(),
	)
	repoA, found, err := store.repositories.GetByFullName(ctx, "example-org/example-alpha")
	if err != nil || !found {
		t.Fatalf("get alpha repository: found=%t err=%v", found, err)
	}
	repoB, found, err := store.repositories.GetByFullName(ctx, "example-org/example-beta")
	if err != nil || !found {
		t.Fatalf("get beta repository: found=%t err=%v", found, err)
	}
	applicationtest.NewCommentArchiveSeeder(t, store.comments, ctx).Many(
		applicationtest.Comment.WithRepositoryID(repoB.ID).WithGitHubCommentID(10).WithCreatedAt(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)).Bare(),
		applicationtest.Comment.WithRepositoryID(repoA.ID).WithGitHubCommentID(2).WithCreatedAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)).WithRuleIDs([]string{"AIR-GO-001"}).Bare(),
		applicationtest.Comment.WithRepositoryID(repoA.ID).WithGitHubCommentID(1).WithCreatedAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)).WithRuleIDs(nil).Bare(),
	)
	return store
}

func exportBytes(t *testing.T, store *fakeExportStore, path string) []byte {
	t.Helper()
	if err := ExportCommentsJSONL(context.Background(), store.repositories, store.comments, path); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func exportLines(t *testing.T) []jsonlComment {
	t.Helper()
	store := newExportStore(t)
	path := filepath.Join(t.TempDir(), "comments.jsonl")
	exportBytes(t, store, path)
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var lines []jsonlComment
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var line jsonlComment
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

func TestCommentExportConversation(t *testing.T) {
	testigo.Run(t, "is deterministic", func(t *testing.T) {
		store := newExportStore(t)
		path := filepath.Join(t.TempDir(), "comments.jsonl")
		first := exportBytes(t, store, path)
		second := exportBytes(t, store, path)
		assert.Equal(t, second, first)
	})

	testigo.Run(t, "leaves only the destination file", func(t *testing.T) {
		store := newExportStore(t)
		directory := t.TempDir()
		exportBytes(t, store, filepath.Join(directory, "comments.jsonl"))
		entries, err := os.ReadDir(directory)
		assert.NoError(t, err)
		assert.Equal(t, []string{entries[0].Name()}, []string{"comments.jsonl"})
	})

	testigo.Run(t, "creates a missing destination directory", func(t *testing.T) {
		store := newExportStore(t)
		path := filepath.Join(t.TempDir(), "nested", "var", "comments.jsonl")
		exportBytes(t, store, path)
		_, err := os.Stat(path)
		assert.NoError(t, err)
	})

	testigo.Run(t, "orders comments by repository date and identity", func(t *testing.T) {
		lines := exportLines(t)
		assert.Equal(t, []int64{lines[0].CommentID, lines[1].CommentID, lines[2].CommentID}, []int64{1, 2, 10})
	})

	testigo.Run(t, "resolves repository names", func(t *testing.T) {
		lines := exportLines(t)
		assert.Equal(t, []string{lines[0].Repository, lines[1].Repository, lines[2].Repository}, []string{"example-org/example-alpha", "example-org/example-alpha", "example-org/example-beta"})
	})

	testigo.Run(t, "exports referenced rule identities", func(t *testing.T) {
		assert.Equal(t, exportLines(t)[1].RuleIDs, []string{"AIR-GO-001"})
	})

	testigo.Run(t, "exports absent rule identities as an empty array", func(t *testing.T) {
		assert.Equal(t, exportLines(t)[0].RuleIDs, []string{})
	})
}
