package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func bearerToken(token []byte) bool {
	if len(token) <= 4 || !bytes.HasPrefix(token, []byte("ikp_")) {
		return false
	}
	for _, b := range token[4:] {
		if b < 0x21 || b > 0x7e {
			return false
		}
	}
	return true
}

func (i *invocation) runToken(set bool) int {
	command := "token"
	if set {
		command = "token set"
	}
	t, err := i.resolve(command, "", false, set)
	if err != nil {
		return i.report(err)
	}
	defer t.unlock()
	path := t.paths.token(t.entry.Name)
	if !set {
		token, readErr := os.ReadFile(filepath.Clean(path))
		if errors.Is(readErr, os.ErrNotExist) {
			if _, writeErr := fmt.Fprintf(i.stderr, "sandbox: no token stored for sandbox '%s'\n\nAsk the human to sign in at http://auth.%s.localhost:%d, create a bearer\ntoken, and store it with: printf '%%s' '<token>' | sandbox token set\n", t.entry.Name, t.entry.Name, t.entry.Port); writeErr != nil {
				return 1
			}
			return 2
		}
		if readErr != nil {
			return i.fileError(path, readErr)
		}
		if !bearerToken(token) {
			return i.fileError(path, errors.New("does not hold a bearer token"))
		}
		if _, writeErr := fmt.Fprintf(i.stdout, "%s\n", token); writeErr != nil {
			return 1
		}
		return 0
	}
	token, err := io.ReadAll(i.stdin)
	if err != nil {
		i.diagnostic("stdin: " + err.Error())
		return 1
	}
	if bytes.HasSuffix(token, []byte("\r\n")) {
		token = token[:len(token)-2]
	} else if bytes.HasSuffix(token, []byte("\n")) {
		token = token[:len(token)-1]
	}
	if len(token) == 0 {
		i.diagnostic("no token on stdin")
		return 2
	}
	if !bearerToken(token) {
		if _, writeErr := fmt.Fprint(i.stderr, "sandbox: stdin does not hold a bearer token\n\na bearer token is one line beginning 'ikp_'\n"); writeErr != nil {
			return 1
		}
		return 2
	}
	data := t.paths.data(t.entry.Name)
	if err = os.MkdirAll(data, 0700); err != nil {
		return i.fileError(data, err)
	}
	info, err := os.Stat(data)
	if err != nil {
		return i.fileError(data, err)
	}
	if !info.IsDir() {
		return i.fileError(data, syscall.ENOTDIR)
	}
	file, err := os.CreateTemp(data, ".token-")
	if err != nil {
		return i.fileError(path, err)
	}
	temp := file.Name()
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(token)
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temp, filepath.Clean(path))
	}
	if err != nil {
		if removeErr := os.Remove(temp); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return i.fileError(path, removeErr)
		}
		return i.fileError(path, err)
	}
	return 0
}
