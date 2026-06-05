package wire_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"agentkit/wire"
)

// R-V0HE-KAIK: stdin EOF means "no more user input will arrive" — not
// "the iteration is over". The reader surfaces EOF as io.EOF (distinct
// from a parse error) and remains safe to call after EOF without
// blocking or panicking. Termination is the driver's job; it ends the
// iteration by emitting a `result` event, never on EOF alone.
func TestR_V0HE_KAIK_StdinEOFSignalsNoMoreInputNotTermination(t *testing.T) {
	t.Run("eof_immediately_on_empty_stream", func(t *testing.T) {
		r := wire.NewStdinReader(strings.NewReader(""))
		_, err := r.Next()
		if !errors.Is(err, io.EOF) {
			t.Fatalf("Next on empty stream: err = %v, want io.EOF", err)
		}
		if !r.EOF() {
			t.Errorf("EOF() = false after io.EOF, want true")
		}
	})

	t.Run("event_then_eof", func(t *testing.T) {
		line := `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}` + "\n"
		r := wire.NewStdinReader(strings.NewReader(line))

		ev, err := r.Next()
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if r.EOF() {
			t.Errorf("EOF() = true before draining stream")
		}
		blk, ok := ev.Message.Content[0].(wire.TextBlock)
		if !ok || blk.Text != "hello" {
			t.Fatalf("first event = %+v, want text=hello", ev)
		}

		_, err = r.Next()
		if !errors.Is(err, io.EOF) {
			t.Fatalf("second Next: err = %v, want io.EOF", err)
		}
	})

	t.Run("eof_is_idempotent", func(t *testing.T) {
		r := wire.NewStdinReader(strings.NewReader(""))
		for i := 0; i < 5; i++ {
			_, err := r.Next()
			if !errors.Is(err, io.EOF) {
				t.Fatalf("Next #%d after EOF: err = %v, want io.EOF", i, err)
			}
		}
	})

	t.Run("multiple_events_then_eof", func(t *testing.T) {
		stream := `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"a"}]}}` + "\n" +
			`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"b"}]}}` + "\n"
		r := wire.NewStdinReader(strings.NewReader(stream))

		want := []string{"a", "b"}
		for i, w := range want {
			ev, err := r.Next()
			if err != nil {
				t.Fatalf("Next #%d: %v", i, err)
			}
			blk := ev.Message.Content[0].(wire.TextBlock)
			if blk.Text != w {
				t.Errorf("event %d text = %q, want %q", i, blk.Text, w)
			}
		}
		_, err := r.Next()
		if !errors.Is(err, io.EOF) {
			t.Fatalf("trailing Next: err = %v, want io.EOF", err)
		}
	})
}
