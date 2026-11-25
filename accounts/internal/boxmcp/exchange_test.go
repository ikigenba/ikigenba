package boxmcp

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// R-PSBY-J567
func TestExchangeReadsRefusalAfterFailedWrite(t *testing.T) {
	request := []byte(`{"jsonrpc":"2.0","id":5,"method":"tools/call"}`)
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "write error", err: errors.New("test socket rejected the request")},
		{name: "short write"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const refusal = "busy"
			var reply bytes.Buffer
			if err := WriteAnswer(&reply, Answer{Refusal: refusal}); err != nil {
				t.Fatal(err)
			}
			socket := &rejectingSocket{reply: &reply, err: tc.err}

			got := exchange(t.Context(), socket, request)
			if !socket.readAfterWrite {
				t.Fatal("daemon's reply was not read after attempting to write the request")
			}
			if got.Refusal != refusal || len(got.Message) != 0 {
				t.Fatalf("answer = %+v, want refusal %q", got, refusal)
			}
		})
	}
}

type rejectingSocket struct {
	reply          io.Reader
	err            error
	writeAttempted bool
	readAfterWrite bool
}

func (s *rejectingSocket) Write(p []byte) (int, error) {
	s.writeAttempted = true
	if s.err != nil {
		return 0, s.err
	}
	return max(len(p)-1, 0), nil
}

func (s *rejectingSocket) Read(p []byte) (int, error) {
	s.readAfterWrite = s.writeAttempted
	return s.reply.Read(p)
}
