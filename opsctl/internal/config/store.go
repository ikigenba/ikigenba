// Package config is the host configuration store.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	// Dir is the host directory that holds the configuration file.
	Dir = "/etc/ikigenba"
	// FileName is the configuration file's base name.
	FileName = "config.json"
)

// ErrNotSet is returned by Get when the key is absent.
var ErrNotSet = errors.New("key not set")

// Store is the config file under one filesystem root (D1 Deps.Root).
type Store struct {
	Root string
}

// Get returns the value of key.
func (s Store) Get(key string) (string, error) {
	m, err := s.load()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrNotSet
		}
		return "", err
	}
	value, ok := m[key]
	if !ok {
		return "", ErrNotSet
	}
	return value, nil
}

// Set stores key to value, creating the directory and file if needed.
func (s Store) Set(key, value string) error {
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
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]string{}
	}
	return m, nil
}

func (s Store) save(m map[string]string) error {
	if err := os.MkdirAll(s.dirPath(), 0o700); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return err
	}
	return os.WriteFile(s.filePath(), buf.Bytes(), 0o600)
}
