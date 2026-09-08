package agentkit

import "context"

// Savepoint is an opaque handle to a point in a Conversation's history.
type Savepoint struct {
	owner *Conversation
	id    uint64
}

// Savepoint returns an opaque handle associated with the Conversation.
func (c *Conversation) Savepoint() (Savepoint, error) {
	if c.state == conversationInFlight {
		return Savepoint{}, ErrTurnInFlight
	}
	if c.isClosed() {
		return Savepoint{}, ErrClosed
	}
	if c.liveSavepoint {
		return Savepoint{}, ErrSavepointActive
	}
	c.savepointGeneration++
	c.savepointHistory = cloneHistory(c.history)
	c.savepointToolCalls = c.toolCallsDispatched
	c.liveSavepoint = true
	log, _ := c.eventSink.(*Log)
	log.savepoint()
	return Savepoint{owner: c, id: c.savepointGeneration}, nil
}

// Restore accepts a Savepoint handle.
func (c *Conversation) Restore(sp Savepoint) error {
	if c.state == conversationInFlight {
		return ErrTurnInFlight
	}
	if c.isClosed() {
		return ErrClosed
	}
	if !c.isLiveSavepoint(sp) {
		return ErrInvalidArgument
	}
	c.history = cloneHistory(c.savepointHistory)
	c.toolCallsDispatched = c.savepointToolCalls
	c.lastRoundContext = 0
	log, _ := c.eventSink.(*Log)
	log.restore()
	return nil
}

// Release accepts a Savepoint handle.
func (c *Conversation) Release(sp Savepoint) error {
	if c.state == conversationInFlight {
		return ErrTurnInFlight
	}
	if c.isClosed() {
		return ErrClosed
	}
	if !c.isLiveSavepoint(sp) {
		return ErrInvalidArgument
	}
	if releaser, ok := c.provider.(cacheReleasingProvider); ok {
		if err := releaser.releaseCache(context.Background()); err != nil {
			return err
		}
	}
	c.liveSavepoint = false
	c.savepointHistory = nil
	log, _ := c.eventSink.(*Log)
	log.release()
	return nil
}

func (c *Conversation) isLiveSavepoint(sp Savepoint) bool {
	return c.liveSavepoint && sp.owner == c && sp.id == c.savepointGeneration
}

// Close marks the Conversation closed without closing its consumer-owned Log.
func (c *Conversation) Close() error {
	if c.state == conversationInFlight {
		return ErrTurnInFlight
	}
	if c.isClosed() {
		return nil
	}
	if releaser, ok := c.provider.(cacheReleasingProvider); ok {
		if err := releaser.releaseCache(context.Background()); err != nil {
			return err
		}
	}
	c.state = conversationClosed
	log, _ := c.eventSink.(*Log)
	log.closeConversation()
	return nil
}
