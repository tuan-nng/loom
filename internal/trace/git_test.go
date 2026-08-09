package trace

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"loom/internal/store"
)

// newGitRepo creates a throwaway repo with an initial commit containing the
// given files, so rev-parse HEAD succeeds. Commits run with -c name/email so
// the tests do not depend on global git config.
func newGitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "--quiet", "-b", "main")
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("trace: write %s: %v", name, err)
		}
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "--quiet", "-m", "init")
	return dir
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=test", "-c", "user.email=t@example.com"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("trace: git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestSnapshotBaseline(t *testing.T) {
	t.Run("clean repo", func(t *testing.T) {
		dir := newGitRepo(t, map[string]string{"a.txt": "one"})
		b, err := SnapshotBaseline(dir)
		if err != nil {
			t.Fatalf("SnapshotBaseline: %v", err)
		}
		if b.BaseHead == "" {
			t.Fatal("BaseHead empty in a repo with a commit")
		}
		if b.Porcelain != "" {
			t.Fatalf("Porcelain = %q, want empty for a clean tree", b.Porcelain)
		}
	})

	t.Run("dirty tree", func(t *testing.T) {
		dir := newGitRepo(t, map[string]string{"a.txt": "one"})
		b, err := SnapshotBaseline(dir)
		if err != nil {
			t.Fatalf("SnapshotBaseline: %v", err)
		}
		for name, edit := range map[string]string{"a.txt": "two"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(edit), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
		}
		cur, err := SnapshotBaseline(dir)
		if err != nil {
			t.Fatalf("SnapshotBaseline: %v", err)
		}
		if cur.BaseHead != b.BaseHead {
			t.Fatalf("BaseHead changed by a worktree edit: %q -> %q", b.BaseHead, cur.BaseHead)
		}
		if !strings.Contains(cur.Porcelain, " M a.txt") {
			t.Fatalf("Porcelain = %q, want it to contain %q", cur.Porcelain, " M a.txt")
		}
	})

	t.Run("untracked", func(t *testing.T) {
		dir := newGitRepo(t, map[string]string{"a.txt": "one"})
		if err := os.WriteFile(filepath.Join(dir, "u.txt"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		cur, err := SnapshotBaseline(dir)
		if err != nil {
			t.Fatalf("SnapshotBaseline: %v", err)
		}
		if !strings.Contains(cur.Porcelain, "?? u.txt") {
			t.Fatalf("Porcelain = %q, want it to contain %q", cur.Porcelain, "?? u.txt")
		}
	})

	t.Run("not a repository", func(t *testing.T) {
		b, err := SnapshotBaseline(t.TempDir())
		if err != nil {
			t.Fatalf("SnapshotBaseline outside a repo: %v", err)
		}
		if b != (Baseline{}) {
			t.Fatalf("Baseline = %+v, want empty", b)
		}
	})

	t.Run("fresh init without commits", func(t *testing.T) {
		dir := t.TempDir()
		git(t, dir, "init", "--quiet", "-b", "main")
		b, err := SnapshotBaseline(dir)
		if err != nil {
			t.Fatalf("SnapshotBaseline in a commitless repo: %v", err)
		}
		if b != (Baseline{}) {
			t.Fatalf("Baseline = %+v, want empty", b)
		}
	})
}

func TestReconcileWorktreeSet(t *testing.T) {
	tests := []struct {
		name string
		base Baseline
		cur  Baseline
		want []Change
	}{
		{
			name: "untracked at completion is created",
			base: Baseline{},
			cur:  Baseline{Porcelain: "?? new.txt\n"},
			want: []Change{{"new.txt", store.OpCreated}},
		},
		{
			name: "clean then modified is created",
			base: Baseline{},
			cur:  Baseline{Porcelain: " M b.txt\n"},
			want: []Change{{"b.txt", store.OpCreated}},
		},
		{
			name: "clean then staged is created",
			base: Baseline{},
			cur:  Baseline{Porcelain: "M  c.txt\n"},
			want: []Change{{"c.txt", store.OpCreated}},
		},
		{
			name: "baseline modified then clean is modified",
			base: Baseline{Porcelain: " M c.txt\n"},
			cur:  Baseline{},
			want: []Change{{"c.txt", store.OpModified}},
		},
		{
			name: "already dirty identical is modified",
			base: Baseline{Porcelain: " M a.txt\n"},
			cur:  Baseline{Porcelain: " M a.txt\n"},
			want: []Change{{"a.txt", store.OpModified}},
		},
		{
			name: "staging-only flip is ambiguous modified",
			base: Baseline{Porcelain: " M a.txt\n"},
			cur:  Baseline{Porcelain: "M  a.txt\n"},
			want: []Change{{"a.txt", store.OpModified}},
		},
		{
			name: "both columns dirty is modified",
			base: Baseline{Porcelain: " M a.txt\n"},
			cur:  Baseline{Porcelain: "MM a.txt\n"},
			want: []Change{{"a.txt", store.OpModified}},
		},
		{
			name: "deletion from clean state is deleted",
			base: Baseline{},
			cur:  Baseline{Porcelain: " D gone.txt\n"},
			want: []Change{{"gone.txt", store.OpDeleted}},
		},
		{
			name: "deletion of already-dirty file is modified",
			base: Baseline{Porcelain: " M f.txt\n"},
			cur:  Baseline{Porcelain: " D f.txt\n"},
			want: []Change{{"f.txt", store.OpModified}},
		},
		{
			name: "multiple paths sort deterministically",
			base: Baseline{Porcelain: " M z.txt\n"},
			cur:  Baseline{Porcelain: "?? a.txt\n M z.txt\n D m.txt\n"},
			want: []Change{{"a.txt", store.OpCreated}, {"m.txt", store.OpDeleted}, {"z.txt", store.OpModified}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Reconcile(tt.base, tt.cur, "")
			if err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Reconcile = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestReconcileCommittedSet(t *testing.T) {
	tests := []struct {
		name string
		base Baseline
		cur  Baseline
		diff string
		want []Change
	}{
		{
			name: "committed add modify delete",
			base: Baseline{BaseHead: "aaa"},
			cur:  Baseline{BaseHead: "bbb"},
			diff: "A\tadd.txt\nM\tmod.txt\nD\tdel.txt\n",
			want: []Change{{"add.txt", store.OpCreated}, {"del.txt", store.OpDeleted}, {"mod.txt", store.OpModified}},
		},
		{
			name: "committed rename deletes source creates destination",
			base: Baseline{BaseHead: "aaa"},
			cur:  Baseline{BaseHead: "bbb"},
			diff: "R100\told.txt\tnew.txt\n",
			want: []Change{{"new.txt", store.OpCreated}, {"old.txt", store.OpDeleted}},
		},
		{
			name: "committed op wins over working-tree set",
			base: Baseline{BaseHead: "aaa", Porcelain: " M dup.txt\n"},
			cur:  Baseline{BaseHead: "bbb", Porcelain: " M dup.txt\n"},
			diff: "D\tdup.txt\n",
			want: []Change{{"dup.txt", store.OpDeleted}},
		},
		{
			name: "no head move ignores diffOut",
			base: Baseline{BaseHead: "aaa"},
			cur:  Baseline{BaseHead: "aaa", Porcelain: "?? new.txt\n"},
			diff: "M\tnew.txt\n",
			want: []Change{{"new.txt", store.OpCreated}},
		},
		{
			name: "non-repo baseline skips committed set",
			base: Baseline{Porcelain: " M x.txt\n"},
			cur:  Baseline{Porcelain: ""},
			diff: "D\tx.txt\n",
			want: []Change{{"x.txt", store.OpModified}},
		},
		{
			name: "head move with empty diff adds nothing",
			base: Baseline{BaseHead: "aaa"},
			cur:  Baseline{BaseHead: "bbb"},
			diff: "",
			want: []Change{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Reconcile(tt.base, tt.cur, tt.diff)
			if err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Reconcile = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestReconcileUntrackedCaptured(t *testing.T) {
	dir := newGitRepo(t, map[string]string{"a.txt": "one"})
	base, err := SnapshotBaseline(dir)
	if err != nil {
		t.Fatalf("SnapshotBaseline: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cur, err := SnapshotBaseline(dir)
	if err != nil {
		t.Fatalf("SnapshotBaseline: %v", err)
	}
	got, err := Reconcile(base, cur, "")
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	want := []Change{{"new.txt", store.OpCreated}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Reconcile = %#v, want %#v", got, want)
	}
}

func TestReconcileCommittedSetEndToEnd(t *testing.T) {
	dir := newGitRepo(t, map[string]string{"a.txt": "one", "b.txt": "two"})
	base, err := SnapshotBaseline(dir)
	if err != nil {
		t.Fatalf("SnapshotBaseline: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "b.txt")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "-m", "changes")
	cur, err := SnapshotBaseline(dir)
	if err != nil {
		t.Fatalf("SnapshotBaseline: %v", err)
	}
	diff, err := GitDiffNameStatus(dir, base.BaseHead, cur.BaseHead)
	if err != nil {
		t.Fatalf("GitDiffNameStatus: %v", err)
	}
	got, err := Reconcile(base, cur, diff)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	want := []Change{{"a.txt", store.OpModified}, {"b.txt", store.OpDeleted}, {"c.txt", store.OpCreated}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Reconcile = %#v, want %#v", got, want)
	}
}

func TestReconcileRenameEndToEnd(t *testing.T) {
	dir := newGitRepo(t, map[string]string{"a.txt": "hello"})
	base, err := SnapshotBaseline(dir)
	if err != nil {
		t.Fatalf("SnapshotBaseline: %v", err)
	}
	git(t, dir, "mv", "a.txt", "b.txt")
	git(t, dir, "commit", "--quiet", "-m", "rename")
	cur, err := SnapshotBaseline(dir)
	if err != nil {
		t.Fatalf("SnapshotBaseline: %v", err)
	}
	if cur.Porcelain != "" {
		t.Fatalf("clean post-commit tree, got porcelain %q", cur.Porcelain)
	}
	diff, err := GitDiffNameStatus(dir, base.BaseHead, cur.BaseHead)
	if err != nil {
		t.Fatalf("GitDiffNameStatus: %v", err)
	}
	if !strings.Contains(diff, "R100\t") {
		t.Fatalf("expected a rename line, got %q", diff)
	}
	got, err := Reconcile(base, cur, diff)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	want := []Change{{"a.txt", store.OpDeleted}, {"b.txt", store.OpCreated}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Reconcile = %#v, want %#v", got, want)
	}
}

func TestParsePorcelain(t *testing.T) {
	m := parsePorcelain(" M a.txt\nM  b.txt\nMM c.txt\n?? u.txt\n D d.txt\nA  e.txt\n")
	want := map[string]byte{
		"a.txt": 'M',
		"b.txt": 'M',
		"c.txt": 'M',
		"u.txt": '?',
		"d.txt": 'D',
		"e.txt": 'A',
	}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("parsePorcelain = %#v, want %#v", m, want)
	}
}

func TestParsePorcelainCRLF(t *testing.T) {
	m := parsePorcelain(" M a.txt\r\n?? u.txt\r\n")
	want := map[string]byte{"a.txt": 'M', "u.txt": '?'}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("parsePorcelain = %#v, want %#v", m, want)
	}
}

func TestParseNameStatus(t *testing.T) {
	got := parseNameStatus("M\tmod.txt\nA\tadd.txt\nD\tdel.txt\nR075\told.txt\tnew.txt\nC100\tcopy.txt\tdst.txt\nT\ttype.txt\n")
	want := []Change{
		{"mod.txt", store.OpModified},
		{"add.txt", store.OpCreated},
		{"del.txt", store.OpDeleted},
		{"old.txt", store.OpDeleted},
		{"new.txt", store.OpCreated},
		{"type.txt", store.OpModified},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseNameStatus = %#v, want %#v", got, want)
	}
}

func TestDedup(t *testing.T) {
	live := []Change{{"a.txt", store.OpModified}}
	changes := []Change{{"a.txt", store.OpModified}, {"b.txt", store.OpCreated}}
	got := Dedup(live, changes)
	want := []Change{{"b.txt", store.OpCreated}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Dedup = %#v, want %#v", got, want)
	}
}

func TestDedupEmptyLive(t *testing.T) {
	changes := []Change{{"a.txt", store.OpCreated}}
	if got := Dedup(nil, changes); !reflect.DeepEqual(got, changes) {
		t.Fatalf("Dedup with empty live = %#v, want unchanged %#v", got, changes)
	}
}

func TestFilesChanged(t *testing.T) {
	changes := []Change{{"a.txt", store.OpModified}, {"b.txt", store.OpCreated}, {"a.txt", store.OpDeleted}}
	if got := FilesChanged(changes); got != 2 {
		t.Fatalf("FilesChanged = %d, want 2", got)
	}
}
