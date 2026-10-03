package git

import (
	"bufio"
	"errors"
	"io"
	"net/http"
	"strings"
)

// ErrHeader identifies a malformed, incomplete, or oversized CGI header.
var ErrHeader = errors.New("invalid git CGI header")

// ReadHeader reads a bounded CGI header and preserves any buffered body bytes.
func ReadHeader(r io.Reader) (status int, header http.Header, body io.Reader, err error) {
	reader := bufio.NewReaderSize(r, CopyBufferSize)
	header = make(http.Header)
	status = http.StatusOK
	line := make([]byte, 0)
	for count := 0; count < MaxHeaderBytes; count++ {
		b, readErr := reader.ReadByte()
		if readErr != nil {
			return 0, nil, nil, ErrHeader
		}
		if b != '\n' {
			line = append(line, b)
			continue
		}
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		if len(line) == 0 {
			return status, header, reader, nil
		}
		name, value, found := strings.Cut(string(line), ":")
		if !found || !validHeaderName(name) {
			return 0, nil, nil, ErrHeader
		}
		value = strings.Trim(value, " ")
		if strings.EqualFold(name, "Status") {
			if len(value) < 3 || value[0] < '1' || value[0] > '9' || value[1] < '0' || value[1] > '9' || value[2] < '0' || value[2] > '9' {
				return 0, nil, nil, ErrHeader
			}
			status = int(value[0]-'0')*100 + int(value[1]-'0')*10 + int(value[2]-'0')
		} else {
			header.Add(name, value)
		}
		line = line[:0]
	}
	return 0, nil, nil, ErrHeader
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		if name[i] <= ' ' || name[i] == ':' || name[i] == 127 {
			return false
		}
	}
	return true
}
