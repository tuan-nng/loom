package agent

import "strings"

func BuildPrompt(c Card) string {
	var b strings.Builder
	b.WriteString(c.Title)
	if s := strings.TrimSpace(c.Description); s != "" {
		b.WriteString("\n\n## Description\n" + s)
	}
	if s := strings.TrimSpace(c.Objective); s != "" {
		b.WriteString("\n\n## Objective\n" + s)
	}
	if s := strings.TrimSpace(c.AcceptanceCriteria); s != "" {
		b.WriteString("\n\n## Acceptance Criteria\n" + s)
	}
	return b.String()
}
