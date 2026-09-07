// Package config is the host configuration store.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// Dir is the host directory that holds the configuration file.
	Dir = "/etc/ikigenba"
	// FileName is the configuration file's base name.
	FileName = "config.json"
	// LockName is the exclusive-lock file's base name.
	LockName = "config.lock"
)

var (
	// ErrNotSet is returned by Get when the key is absent.
	ErrNotSet = errors.New("key not set")
	// ErrInvalidKey is returned by Set when the key is rejected.
	ErrInvalidKey = errors.New("invalid key")
	// ErrInvalidValue is returned by Set when the value contains a newline.
	ErrInvalidValue = errors.New("invalid value")
	// ErrCorrupt is returned when the configuration file is not a JSON object of strings.
	ErrCorrupt = errors.New("config file is corrupt")
)

// Store is the config file under one filesystem root (D1 Deps.Root).
type Store struct {
	Root string
}

// Entry is one key and value from the store.
type Entry struct {
	Key   string
	Value string
}

// ValidKey reports whether key is a non-empty string matching ^[a-z0-9_.-]+$.
func ValidKey(key string) bool {
	if key == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_', c == '.', c == '-':
		default:
			return false
		}
	}
	return true
}

// Get returns the value of key. The error wraps ErrNotSet when the key is absent.
func (s Store) Get(key string) (string, error) {
	m, err := s.load()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%w: %s", ErrNotSet, key)
		}
		return "", err
	}
	value, ok := m[key]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrNotSet, key)
	}
	return value, nil
}

// Set stores key to value, creating the directory and file if needed.
func (s Store) Set(key, value string) error {
	if !ValidKey(key) {
		return fmt.Errorf("%w", ErrInvalidKey)
	}
	if strings.ContainsAny(value, "\n\r") {
		return fmt.Errorf("%w", ErrInvalidValue)
	}
	m, err := s.load()
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		m = map[string]string{}
	}
	m[key] = value
	return s.save(m)
}

// Del removes key. It is nil whether or not the key was set.
func (s Store) Del(key string) error {
	m, err := s.load()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if _, ok := m[key]; !ok {
		return nil
	}
	delete(m, key)
	return s.save(m)
}

// List returns every entry sorted by Key.
func (s Store) List() ([]Entry, error) {
	m, err := s.load()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Entry{}, nil
		}
		return nil, err
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]Entry, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, Entry{Key: key, Value: m[key]})
	}
	return entries, nil
}

func (s Store) dirPath() string {
	return filepath.Join(s.Root, filepath.FromSlash(strings.TrimPrefix(Dir, "/")))
}

func (s Store) filePath() string {
	return filepath.Join(s.dirPath(), FileName)
}

func (s Store) load() (map[string]string, error) {
	data, err := os.ReadFile(s.filePath())
	if err != nil {
		return nil, err
	}
	m, err := decodeStore(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", s.filePath(), ErrCorrupt)
	}
	return m, nil
}

func (s Store) save(m map[string]string) error {
	dir := s.dirPath()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := encodeStore(m)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, FileName+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, s.filePath()); err != nil {
		return err
	}
	ok = true
	return nil
}

func encodeStore(m map[string]string) ([]byte, error) {
	if m == nil {
		m = map[string]string{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeStore(data []byte) (map[string]string, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var m map[string]string
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errors.New("not a JSON object")
	}
	tok, err := dec.Token()
	if errors.Is(err, io.EOF) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("trailing token %v", tok)
}
