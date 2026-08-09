package agent

import "strings"

func PosixEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func CommandLine(argv []string) string {
	out := make([]string, len(argv))
	for i, a := range argv {
		out[i] = PosixEscape(a)
	}
	return strings.Join(out, " ")
}
