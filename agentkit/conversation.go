package agentkit

import (
	"context"
	"errors"
	"io"
	"net/http"
)

// Conversation binds a provider and its stable identity to a growing
// transcript.
type Conversation struct {
	provider Provider
	client   *http.Client
	identity Identity
	blocks   []Block
}

// NewConversation constructs a conversation driven by provider. The provider,
// its identity, and the HTTP client cannot be reassigned after construction.
func NewConversation(provider Provider, client *http.Client) *Conversation {
	return &Conversation{
		provider: provider,
		client:   client,
		identity: provider.Identity(),
	}
}

// Send drives one turn: it appends the user blocks, calls the model, runs any
// tool round-trips to completion, and returns a Stream of message-granular
// events (D13). Provider, endpoint, and model are fixed for the conversation's
// life; only the transcript grows. Send is the one verb — multimodal input
// arrives as additional Block variants, never as a second method.
func (c *Conversation) Send(ctx context.Context, blocks ...Block) *Stream {
	c.blocks = append(c.blocks, blocks...)

	return c.roundTrip(ctx)
}

func (c *Conversation) roundTrip(ctx context.Context) *Stream {
	request, err := c.buildRequest(ctx)
	if err != nil {
		return &Stream{err: err}
	}

	response, err := c.execute(request)
	if err != nil {
		return &Stream{err: err}
	}

	return c.consumeResponse(ctx, response)
}

func (c *Conversation) buildRequest(ctx context.Context) (*http.Request, error) {
	state := RequestState{
		Model:  c.identity.Model,
		Blocks: append([]Block(nil), c.blocks...),
	}

	return c.provider.BuildRequest(ctx, state)
}

func (c *Conversation) execute(request *http.Request) (*http.Response, error) {
	if c.client == nil {
		return nil, errors.New("agentkit: nil HTTP client")
	}

	return c.client.Do(request)
}

func (c *Conversation) consumeResponse(ctx context.Context, response *http.Response) *Stream {
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			return &Stream{err: readErr}
		}

		return &Stream{err: c.provider.Classify(response.StatusCode, response.Header, body)}
	}

	stream := &Stream{}
	for event, decodeErr := range c.provider.Decode(ctx, response) {
		if decodeErr != nil {
			stream.err = decodeErr

			break
		}
		stream.events = append(stream.events, event)
	}

	return stream
}
