package trace

import "testing"

func TestParseIgnorePattern(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		ok      bool
		dirOnly bool
		negate  bool
	}{
		{name: "blank line", line: "", ok: false},
		{name: "whitespace only", line: "   ", ok: false},
		{name: "comment", line: "# build artifacts", ok: false},
		{name: "simple pattern", line: "*.log", ok: true},
		{name: "negated", line: "!keep.log", ok: true, negate: true},
		{name: "dir only", line: "build/", ok: true, dirOnly: true},
		{name: "empty after negate", line: "!", ok: false},
		{name: "empty after slash", line: "/", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ok := parseIgnorePattern(tt.line)
			if ok != tt.ok {
				t.Fatalf("parseIgnorePattern(%q) ok = %v, want %v", tt.line, ok, tt.ok)
			}
			if !ok {
				return
			}
			if p.dirOnly != tt.dirOnly {
				t.Errorf("parseIgnorePattern(%q) dirOnly = %v, want %v", tt.line, p.dirOnly, tt.dirOnly)
			}
			if p.negate != tt.negate {
				t.Errorf("parseIgnorePattern(%q) negate = %v, want %v", tt.line, p.negate, tt.negate)
			}
		})
	}
}

func TestIgnoreMatcher(t *testing.T) {
	tests := []struct {
		name    string
		lines   []string
		path    string
		isDir   bool
		ignored bool
	}{
		{name: "no patterns -> not ignored", lines: nil, path: "a.txt", ignored: false},
		{name: "extension anywhere", lines: []string{"*.log"}, path: "a/b/x.log", ignored: true},
		{name: "extension must be a file segment", lines: []string{"*.log"}, path: "a/b", ignored: false},
		{name: "single-star does not cross slash", lines: []string{"doc/*.md"}, path: "doc/x.md", ignored: true},
		{name: "single-star does not cross slash, deep", lines: []string{"doc/*.md"}, path: "doc/sub/x.md", ignored: false},
		{name: "dir-only pattern on dir", lines: []string{"build/"}, path: "a/build", isDir: true, ignored: true},
		{name: "dir-only pattern on file", lines: []string{"build/"}, path: "a/build", isDir: false, ignored: false},
		{name: "anchored to root", lines: []string{"/build/"}, path: "build", isDir: true, ignored: true},
		{name: "anchored does not match deep", lines: []string{"/build/"}, path: "a/build", isDir: true, ignored: false},
		{name: "mid-slash pattern is anchored", lines: []string{"a/build/"}, path: "a/build", isDir: true, ignored: true},
		{name: "mid-slash anchored not deep", lines: []string{"a/build/"}, path: "x/a/build", isDir: true, ignored: false},
		{name: "double-star any depth", lines: []string{"**/config.go"}, path: "x/y/config.go", ignored: true},
		{name: "double-star any depth, deep", lines: []string{"**/config.go"}, path: "config.go", ignored: true},
		{name: "mid double-star", lines: []string{"a/**/b"}, path: "a/x/y/b", ignored: true},
		{name: "mid double-star, zero levels", lines: []string{"a/**/b"}, path: "a/b", ignored: true},
		{name: "negation re-includes", lines: []string{"*.log", "!keep.log"}, path: "keep.log", ignored: false},
		{name: "negation then re-ignore, last wins", lines: []string{"*.log", "!keep.log", "keep.log"}, path: "keep.log", ignored: true},
		{name: "negation only matches its own shape", lines: []string{"*.log", "!keep.log"}, path: "drop.log", ignored: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newIgnoreMatcher(tt.lines)
			if got := m.match(tt.path, tt.isDir); got != tt.ignored {
				t.Errorf("match(%q, isDir=%v) = %v, want %v", tt.path, tt.isDir, got, tt.ignored)
			}
		})
	}
}

func TestIgnoredPathBuiltins(t *testing.T) {
	m := newIgnoreMatcher(nil)
	tests := []struct {
		name    string
		rel     string
		isDir   bool
		ignored bool
	}{
		{name: ".git at root", rel: ".git", isDir: true, ignored: true},
		{name: ".git nested", rel: "a/b/.git", isDir: true, ignored: true},
		{name: "node_modules nested", rel: "src/node_modules", isDir: true, ignored: true},
		{name: "dist nested", rel: "public/dist", isDir: true, ignored: true},
		{name: "build nested", rel: "a/build", isDir: true, ignored: true},
		{name: "vendor nested", rel: "a/vendor", isDir: true, ignored: true},
		{name: ".venv nested", rel: "a/.venv", isDir: true, ignored: true},
		{name: "__pycache__ nested", rel: "a/__pycache__", isDir: true, ignored: true},
		{name: "file named dist not ignored", rel: "dist", isDir: false, ignored: false},
		{name: "file named build not ignored", rel: "build", isDir: false, ignored: false},
		{name: "normal dir not ignored", rel: "src", isDir: true, ignored: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ignoredPath(m, tt.rel, tt.isDir); got != tt.ignored {
				t.Errorf("ignoredPath(%q, isDir=%v) = %v, want %v", tt.rel, tt.isDir, got, tt.ignored)
			}
		})
	}
}

func TestBuiltinIgnoresNotNegatable(t *testing.T) {
	// ADR-001 §4.6 rule 1: built-in ignores are not configurable off. A
	// .loomignore "!node_modules" must not re-include the directory.
	m := newIgnoreMatcher([]string{"!node_modules"})
	if !ignoredPath(m, "node_modules", true) {
		t.Fatal("built-in .git/node_modules must not be re-included by .loomignore")
	}
}
