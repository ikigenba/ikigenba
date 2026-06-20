package llm

import agentkit "github.com/ikigenba/agentkit"

// AgentKitProvider is the production provider boundary supplied at composition.
type AgentKitProvider = agentkit.Provider

// ToAgentKit converts the narrow wiki prompt shape to AgentKit messages.
func ToAgentKit(messages []Message) []agentkit.Message {
	out := make([]agentkit.Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, agentkit.Message{
			Role:   agentkit.Role(msg.Role),
			Blocks: []agentkit.Block{agentkit.TextBlock{Text: msg.Content}},
		})
	}
	return out
}
