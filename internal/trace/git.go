// Package trace reconciles a run's file changes against a git baseline pair
// (ADR-001 §5, DESIGN-002 §10.2): pure logic over porcelain snapshots plus
// the committed set computed from a HEAD move. fsnotify wiring is T9.
package trace

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"loom/internal/store"
)

// Baseline is a git state pair captured at a run boundary (ADR-001 §3.3,
// §5). BaseHead comes from `git rev-parse HEAD`, Porcelain from
// `git status --porcelain`. Both are empty when root is not inside a git
// repository; store.StartRun omits the git key on empty BaseHead
// (traces.go).
type Baseline struct {
	BaseHead  string
	Porcelain string
}

// Change attributes one path's operation to a run (DESIGN-002 §10.2).
// Operation is one of store.OpCreated, store.OpModified, store.OpDeleted.
type Change struct {
	Path      string
	Operation string
}

// gitError carries git's exit code and stderr so callers can classify a
// "not a git repository" failure without string-matching a generic exec
// error.
type gitError struct {
	code   int
	stderr string
}

func (e *gitError) Error() string {
	return fmt.Sprintf("trace: git exited %d: %s", e.code, strings.TrimSpace(e.stderr))
}

// SnapshotBaseline captures the baseline pair for root via short-lived exec
// git clients (ADR-001 §5). Outside a git repository, or inside one with no
// commits yet, it returns an empty Baseline and no error — the store
// represents that state as "no git key" (traces.go). Failure is reserved for
// git failing for any other reason.
func SnapshotBaseline(root string) (Baseline, error) {
	head, err := gitOut(root, "rev-parse", "HEAD")
	if err != nil {
		if notARepo(err) {
			return Baseline{}, nil
		}
		return Baseline{}, err
	}
	porcelain, err := gitOut(root, "status", "--porcelain")
	if err != nil {
		return Baseline{}, err
	}
	return Baseline{BaseHead: strings.TrimSpace(head), Porcelain: porcelain}, nil
}

// Reconcile computes the authoritative change set for a run's completion
// (ADR-001 §5). It is exec-free: the committed set is parsed from diffOut,
// the text of `git diff --name-status <baseHead> <currentHead>` which the
// caller supplies (see GitDiffNameStatus).
//
// The path-keyed working-tree set is the primary source: a path in the
// completion snapshot alone is created, unless its normalized letter denotes
// deletion (the file was deleted from a clean state). Any path present at
// baseline is attributed modified — a staging-only letter flip such as
// ` M`→`M ` is indistinguishable from real modification, so the ambiguous
// case is over-attributed rather than dropped (ADR-001 §5 changelog
// 2026-08-08). When HEAD moved, the committed set's paths and operations
// win over the working-tree set on conflict.
func Reconcile(baseline, current Baseline, diffOut string) ([]Change, error) {
	base := parsePorcelain(baseline.Porcelain)
	cur := parsePorcelain(current.Porcelain)

	out := make(map[string]Change)
	for path, letter := range cur {
		if _, ok := base[path]; !ok {
			op := store.OpCreated
			if letter == 'D' {
				op = store.OpDeleted
			}
			out[path] = Change{Path: path, Operation: op}
		}
	}
	for path := range base {
		out[path] = Change{Path: path, Operation: store.OpModified}
	}
	if baseline.BaseHead != "" && baseline.BaseHead != current.BaseHead {
		for _, c := range parseNameStatus(diffOut) {
			out[c.Path] = c
		}
	}

	changes := make([]Change, 0, len(out))
	for _, c := range out {
		changes = append(changes, c)
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

// Dedup returns the reconcile changes whose paths fsnotify did not already
// record live, preserving order (DESIGN-002 §10.2: a path is emitted only if
// the live recorder did not already write it). live is the watcher-recorded
// set; empty or nil live returns changes unchanged.
func Dedup(live []Change, changes []Change) []Change {
	if len(live) == 0 {
		return changes
	}
	seen := make(map[string]struct{}, len(live))
	for _, c := range live {
		seen[c.Path] = struct{}{}
	}
	out := make([]Change, 0, len(changes))
	for _, c := range changes {
		if _, ok := seen[c.Path]; ok {
			continue
		}
		out = append(out, c)
	}
	return out
}

// FilesChanged counts unique paths across changes — the value recorded in
// trace_end.data_json.files_changed (ADR-001 §3.3).
func FilesChanged(changes []Change) int {
	seen := make(map[string]struct{}, len(changes))
	for _, c := range changes {
		seen[c.Path] = struct{}{}
	}
	return len(seen)
}

// GitDiffNameStatus produces the committed-set text Reconcile consumes: the
// name-status diff between the run's base and current heads (ADR-001 §5). The
// session manager supplies it so Reconcile stays exec-free.
func GitDiffNameStatus(root, baseHead, head string) (string, error) {
	return gitOut(root, "diff", "--name-status", baseHead, head)
}

// gitOut runs git in dir and returns stdout; a non-zero exit surfaces a
// gitError so callers can classify the failure.
func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", &gitError{code: ee.ExitCode(), stderr: stderr.String()}
		}
		return "", fmt.Errorf("trace: git %v: %v", args, err)
	}
	return stdout.String(), nil
}

// notARepo reports the two fatal 128 cases git emits outside a usable repo:
// the root is not inside one, or exists but has no commits yet. Both are the
// "no baseline" state for SnapshotBaseline (ADR-001 §3.3).
func notARepo(err error) bool {
	ge, ok := err.(*gitError)
	if !ok || ge.code != 128 {
		return false
	}
	return strings.Contains(ge.stderr, "not a git repository") ||
		strings.Contains(ge.stderr, "unknown revision")
}

// parsePorcelain builds the path→letter map for a porcelain snapshot. The
// letter is normalized so staging does not distinguish worktree from index
// modification: any M in either column maps to 'M', so " M"≡"M "≡"MM" — a
// staging-only flip lands in the ambiguous already-dirty bucket, not a new
// change. 'D' in either column dominates.
func parsePorcelain(out string) map[string]byte {
	m := make(map[string]byte)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if len(line) < 4 || line[2] != ' ' {
			continue
		}
		m[line[3:]] = normalizeLetter(line[:2])
	}
	return m
}

func normalizeLetter(xy string) byte {
	for i := 0; i < 2; i++ {
		if xy[i] == 'D' {
			return 'D'
		}
	}
	for i := 0; i < 2; i++ {
		switch xy[i] {
		case 'M':
			return 'M'
		case '?':
			return '?'
		}
	}
	return 'A'
}

// parseNameStatus converts `git diff --name-status` text into committed-set
// changes (ADR-001 §5 step 2): A→created, M→modified, D→deleted, T→modified
// (type change is a content change), and a rename (R<score>, source path
// first) into source deleted + destination created.
func parseNameStatus(out string) []Change {
	var changes []Change
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(strings.TrimSpace(line), "\t")
		if len(fields) < 2 {
			continue
		}
		switch strings.TrimRight(fields[0], "0123456789") {
		case "A":
			changes = append(changes, Change{Path: fields[1], Operation: store.OpCreated})
		case "M", "T":
			changes = append(changes, Change{Path: fields[1], Operation: store.OpModified})
		case "D":
			changes = append(changes, Change{Path: fields[1], Operation: store.OpDeleted})
		case "R":
			if len(fields) < 3 {
				continue
			}
			changes = append(changes, Change{Path: fields[1], Operation: store.OpDeleted})
			changes = append(changes, Change{Path: fields[2], Operation: store.OpCreated})
		}
	}
	return changes
}
