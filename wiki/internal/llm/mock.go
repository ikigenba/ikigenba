package llm

import "context"

// MockProvider is a deterministic provider for tests.
type MockProvider struct {
	Responses []string
	Calls     [][]Message
}

func (m *MockProvider) Complete(_ context.Context, messages []Message) (string, error) {
	copied := append([]Message(nil), messages...)
	m.Calls = append(m.Calls, copied)
	if len(m.Responses) == 0 {
		return "", nil
	}
	out := m.Responses[0]
	m.Responses = m.Responses[1:]
	return out, nil
}
