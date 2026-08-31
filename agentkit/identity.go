package agentkit

// Identity is a conversation's stable provenance.
type Identity struct {
	Endpoint string
	AuthMode string
	Model    string
}
