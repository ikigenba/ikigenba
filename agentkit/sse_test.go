package agentkit

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type failingReader struct {
	data []byte
	err  error
}

func (r *failingReader) Read(destination []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(destination, r.data)
	r.data = r.data[n:]
	return n, nil
}

type byteReader struct {
	data  []byte
	reads int
}

func (r *byteReader) Read(destination []byte) (int, error) {
	r.reads++
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	destination[0] = r.data[0]
	r.data = r.data[1:]
	return 1, nil
}

func TestSSEFramesIsStandalonePublicLeaf(t *testing.T) {
	// R-33OD-EUS2
	input := ": keep-alive\r\nevent: ignored\r\ndata: first\r\ndata: second\r\n\r\n" +
		"data:\n\n\n" +
		"data: final"
	var frames [][]byte
	for frame, err := range SSEFrames(strings.NewReader(input)) {
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, frame)
	}
	want := []string{"first\nsecond", "", "final"}
	if len(frames) != len(want) {
		t.Fatalf("frames = %q, want %q", frames, want)
	}
	for index := range want {
		if string(frames[index]) != want[index] {
			t.Fatalf("frame %d = %q, want %q", index, frames[index], want[index])
		}
	}
}

func TestSSEFramesStopsAtTerminalSentinel(t *testing.T) {
	input := "data: before\n\ndata: [DONE]\n\ndata: after\n\n"
	var frames []string
	for frame, err := range SSEFrames(strings.NewReader(input)) {
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, string(frame))
	}
	if len(frames) != 1 || frames[0] != "before" {
		t.Fatalf("frames = %q, want only pre-sentinel payload", frames)
	}
}

func TestSSEFramesPropagatesReaderFailure(t *testing.T) {
	want := errors.New("read failed")
	reader := &failingReader{data: []byte("data: partial"), err: want}
	var got error
	for _, err := range SSEFrames(reader) {
		got = err
	}
	if !errors.Is(got, want) {
		t.Fatalf("error = %v, want %v", got, want)
	}
}

func TestSSEFramesHonorsEarlyConsumerStop(t *testing.T) {
	reader := &byteReader{data: []byte("data: first\n\ndata: second\n\n")}
	for range SSEFrames(reader) {
		break
	}
	if reader.reads >= len("data: first\n\ndata: second\n\n") {
		t.Fatalf("reader performed %d reads after consumer stopped", reader.reads)
	}
}

func TestSSEFramesYieldsOwnedPayloadBytes(t *testing.T) {
	var frames [][]byte
	for frame, err := range SSEFrames(strings.NewReader("data: first\n\ndata: second\n\n")) {
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, frame)
	}
	frames[0][0] = 'X'
	if string(frames[1]) != "second" {
		t.Fatalf("second frame aliases first: %q", frames[1])
	}
}
