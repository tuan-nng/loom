package agent

type Card struct {
	ID                 string
	Title              string
	Description        string
	Objective          string
	AcceptanceCriteria string
	Agent              string // already resolved: card.agent ?? [agent] default; "" never sent here
}

func (c Card) AgentOrDefault(def string) string {
	if c.Agent != "" {
		return c.Agent
	}
	return def
}
