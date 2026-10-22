package ghcli

import (
	"context"
	"strings"
	"testing"
)

func TestListDirectoryAtSHAPinsRevisionAndReturnsOnlyFiles(t *testing.T) {
	for _, directory := range []string{"", "src"} {
		g := newTestGitHub(func(_ context.Context, name string, args ...string) ([]byte, []byte, int, error) {
			endpoint := "repos/owner/repo/contents"
			if directory != "" {
				endpoint += "/" + directory
			}
			if name != "gh" || !strings.Contains(strings.Join(args, " "), endpoint+"?ref=head-sha") {
				t.Fatalf("directory read was not pinned to HEAD: %s %v", name, args)
			}
			return []byte(`[{"path":"src/main.go","type":"file"},{"path":"src/subdir","type":"dir"}]`), nil, 0, nil
		})
		got, err := g.ListDirectoryAtSHA(context.Background(), "owner/repo", directory, "head-sha")
		if err != nil || len(got) != 1 || got[0] != "src/main.go" {
			t.Fatalf("unexpected directory entries: %v, %v", got, err)
		}
	}
}

func TestChangedFilesRetainsRenameSourcePath(t *testing.T) {
	g := newTestGitHub(newScriptedRunner(t, scriptedCall{stdout: []byte(`[{"filename":"new.go","previous_filename":"old.go","status":"renamed","patch":"@@ -1 +1 @@\n-old\n+new"}]`)}))
	got, err := g.ListChangedFiles(context.Background(), "owner/repo", 7)
	if err != nil || len(got) != 1 || got[0].PreviousPath != "old.go" {
		t.Fatalf("rename source path was lost: %#v, %v", got, err)
	}
}
