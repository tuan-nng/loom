// Ignore-rule handling for the trace watcher (ADR-001 §4.6): the built-in
// default directory ignores plus a hand-rolled gitignore-subset matcher for
// `.loomignore`. The matcher is a pure function of (path, isDir) — no
// filesystem access — so it is table-tested in isolation.
package trace

import (
	"regexp"
	"strings"
)

// builtinIgnores are directory names always skipped when registering watches,
// not configurable off (ADR-001 §4.6 rule 1). They are matched by basename at
// any depth, so a build/ or node_modules/ nested anywhere under the watch
// scope is never descended into.
var builtinIgnores = []string{
	".git", "node_modules", "target", "dist", "build",
	"vendor", ".venv", "__pycache__",
}

// ignorePattern is one compiled line of `.loomignore` (ADR-001 §4.6 rule 2).
// re matches a full scope-relative path; dirOnly restricts to directories;
// negate marks a "!" line that re-includes a previously-ignored path.
type ignorePattern struct {
	re      *regexp.Regexp
	dirOnly bool
	negate  bool
}

// parseIgnorePattern compiles one .loomignore line; ok is false for blank
// lines and '#' comments. The supported subset (deliberately not full
// gitignore — no '?', no bracketed char classes, no backslash escaping):
//   - leading "!" negates (re-include)
//   - trailing "/" matches directories only
//   - a leading "/" anchors the pattern to the scope root; a pattern with a
//     slash anywhere else is anchored as well (git behavior)
//   - "*" matches within one path segment, "**" across segments,
//     a leading "**/" matches at any depth
func parseIgnorePattern(line string) (ignorePattern, bool) {
	line = strings.TrimRight(line, " \t")
	if line == "" || strings.HasPrefix(line, "#") {
		return ignorePattern{}, false
	}

	ip := ignorePattern{}
	if strings.HasPrefix(line, "!") {
		ip.negate = true
		line = strings.TrimPrefix(line, "!")
	}
	if strings.HasSuffix(line, "/") {
		ip.dirOnly = true
		line = strings.TrimSuffix(line, "/")
	}
	if line == "" {
		return ignorePattern{}, false
	}

	// Anchoring follows git: a pattern containing a slash anywhere (outside a
	// leading "**/") is anchored to the scope root, matching a single
	// relative path. A slash-less pattern matches at any depth — the
	// (?:.*/)? prefix on the compiled regex. A leading "/" is explicit
	// anchoring; a leading "**/" means "in all directories".
	anchored := true
	switch {
	case strings.HasPrefix(line, "/"):
		line = strings.TrimPrefix(line, "/")
	case strings.HasPrefix(line, "**/"):
		line = strings.TrimPrefix(line, "**/")
		anchored = false
	default:
		anchored = strings.Contains(line, "/")
	}

	var b strings.Builder
	if anchored {
		b.WriteString("^")
	} else {
		b.WriteString("(?:.*/)?")
	}
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '*':
			if i+1 < len(line) && line[i+1] == '*' {
				if i+2 < len(line) && line[i+2] == '/' {
					b.WriteString("(?:.*/)")
					b.WriteString("?")
					i += 2
				} else {
					b.WriteString(".*")
					i++
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '/':
			b.WriteString("/")
		default:
			b.WriteString(regexp.QuoteMeta(line[i : i+1]))
		}
	}
	b.WriteString("$")

	re, err := regexp.Compile(b.String())
	if err != nil {
		return ignorePattern{}, false
	}
	ip.re = re
	return ip, true
}

// matches reports whether this pattern applies to (path, isDir). dirOnly
// patterns never match a non-directory.
func (ip ignorePattern) matches(path string, isDir bool) bool {
	if ip.dirOnly && !isDir {
		return false
	}
	return ip.re.MatchString(path)
}

// ignoreMatcher is the compiled .loomignore file, preserving line order so
// matching is last-match-wins.
type ignoreMatcher struct {
	patterns []ignorePattern
}

// newIgnoreMatcher compiles the lines of a .loomignore file; blank and
// comment lines are dropped.
func newIgnoreMatcher(lines []string) *ignoreMatcher {
	m := &ignoreMatcher{}
	for _, line := range lines {
		if p, ok := parseIgnorePattern(line); ok {
			m.patterns = append(m.patterns, p)
		}
	}
	return m
}

// match implements gitignore last-match-wins semantics: the last line that
// matches decides — a matching "!" line un-ignores, a matching un-negated
// line ignores. No match means not ignored.
func (m *ignoreMatcher) match(path string, isDir bool) bool {
	ignored := false
	for _, p := range m.patterns {
		if p.matches(path, isDir) {
			ignored = !p.negate
		}
	}
	return ignored
}

// ignoredPath applies the built-in directory ignores (by basename, dirs only
// — rule 1, never re-includable) and then the .loomignore matcher (rule 2).
func ignoredPath(m *ignoreMatcher, rel string, isDir bool) bool {
	if isDir {
		base := rel
		if i := strings.LastIndex(rel, "/"); i >= 0 {
			base = rel[i+1:]
		}
		for _, d := range builtinIgnores {
			if base == d {
				return true
			}
		}
	}
	return m != nil && m.match(rel, isDir)
}
