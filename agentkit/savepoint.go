package agentkit

// Savepoint is an opaque handle to a point in a Conversation's history.
type Savepoint struct {
	id uint64
}

// Savepoint returns an opaque handle associated with the Conversation.
func (c *Conversation) Savepoint() (Savepoint, error) {
	return Savepoint{id: 1}, nil
}

// Restore accepts a Savepoint handle.
func (c *Conversation) Restore(_ Savepoint) error {
	return nil
}

// Release accepts a Savepoint handle.
func (c *Conversation) Release(_ Savepoint) error {
	return nil
}

// Close marks the Conversation closed without closing its consumer-owned Log.
func (c *Conversation) Close() error {
	c.closed = true
	return nil
}
