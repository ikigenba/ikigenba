package accounts

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/opsctl/internal/hardening"
)

// KeyType is the only supported SSH public key type.
const KeyType = "ssh-ed25519"

// Key is an ed25519 public key and its optional label.
type Key struct {
	Body    string
	Comment string
}

// Line returns the canonical authorized_keys line.
func (k Key) Line() string {
	line := KeyType + " " + k.Body
	if k.Comment != "" {
		line += " " + k.Comment
	}
	return line
}

// ParseKey validates an ed25519 OpenSSH public key line.
func ParseKey(s string) (Key, error) {
	invalid := errors.New("not an ed25519 OpenSSH public key")
	if !utf8.ValidString(s) {
		return Key{}, invalid
	}
	for _, b := range []byte(s) {
		if (b < 32 && b != '\t') || b == 127 {
			return Key{}, invalid
		}
	}
	s = strings.Trim(s, " \t")
	typ, rest, found := cutKeyField(s)
	if !found || typ != KeyType {
		return Key{}, invalid
	}
	body, comment, _ := cutKeyField(rest)
	decoded, err := base64.StdEncoding.Strict().DecodeString(body)
	if err != nil || len(decoded) != 51 || string(decoded[:19]) != "\x00\x00\x00\x0bssh-ed25519\x00\x00\x00\x20" {
		return Key{}, invalid
	}
	return Key{Body: body, Comment: comment}, nil
}

func cutKeyField(s string) (string, string, bool) {
	i := strings.IndexAny(s, " \t")
	if i < 0 {
		return s, "", false
	}
	return s[:i], strings.TrimLeft(s[i:], " \t"), true
}

func keyPath(root, name string) string { return rootPath(root, hardening.KeysDir+"/"+name) }

func readKeys(root, name string) ([]string, error) {
	data, err := os.ReadFile(keyPath(root, name))
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	lines := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

func keyMatches(line string, key Key) bool {
	typ, rest, found := cutKeyField(strings.TrimLeft(line, " \t"))
	if !found {
		return false
	}
	body, _, _ := cutKeyField(rest)
	return typ == KeyType && body == key.Body
}

func writeKeys(root, name string, lines []string) error {
	data := ""
	if len(lines) > 0 {
		data = strings.Join(lines, "\n") + "\n"
	}
	return writeKeyBytes(keyPath(root, name), []byte(data), 0644)
}

func writeKeyBytes(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	_, statErr := os.Stat(dir)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if errors.Is(statErr, os.ErrNotExist) {
		directory, err := os.OpenRoot(dir)
		if err != nil {
			return err
		}
		err = directory.Chmod(".", 0755)
		closeErr := directory.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".key-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
