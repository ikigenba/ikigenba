package agentkit

import (
	"bufio"
	"bytes"
	"io"
	"iter"
)

// SSEFrames reads Server-Sent Events and yields fresh copies of their joined
// data payloads. It deliberately does not interpret payload JSON.
func SSEFrames(r io.Reader) iter.Seq2[[]byte, error] {
	return func(yield func([]byte, error) bool) {
		reader := bufio.NewReader(r)
		var data [][]byte
		emit := func() bool {
			if len(data) == 0 {
				return true
			}
			payload := bytes.Join(data, []byte{'\n'})
			data = data[:0]
			if bytes.Equal(payload, []byte("[DONE]")) {
				return false
			}
			return yield(append([]byte(nil), payload...), nil)
		}

		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				line = bytes.TrimSuffix(line, []byte{'\n'})
				line = bytes.TrimSuffix(line, []byte{'\r'})
				switch {
				case len(line) == 0:
					if !emit() {
						return
					}
				case line[0] == ':':
				case bytes.Equal(line, []byte("data")):
					data = append(data, []byte{})
				case bytes.HasPrefix(line, []byte("data:")):
					value := line[len("data:"):]
					if len(value) > 0 && value[0] == ' ' {
						value = value[1:]
					}
					data = append(data, append([]byte(nil), value...))
				}
			}

			switch err {
			case nil:
				continue
			case io.EOF:
				if !emit() {
					return
				}
				return
			default:
				yield(nil, err)
				return
			}
		}
	}
}
