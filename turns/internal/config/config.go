// Package config reads turns' user and host configuration.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ikigenba/ikigenba/turns/internal/diagnostic"
	"github.com/ikigenba/ikigenba/turns/internal/paths"
)

// Config contains the effective space, token, and host default.
type Config struct {
	Space     string
	Token     string
	HostSpace string
}

// Configured reports whether both a space and a token are present.
func (c Config) Configured() bool { return c.Space != "" && c.Token != "" }

// ValidSpace reports whether s is a lowercase, multi-label DNS name.
func ValidSpace(s string) bool {
	if len(s) > 253 {
		return false
	}
	labels := strings.Split(s, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := range len(label) {
			b := label[i]
			if (b < 'a' || b > 'z') && (b < '0' || b > '9') && b != '-' {
				return false
			}
		}
	}
	return true
}

// Load reads the files named by p, treating only absent files as missing.
func Load(p paths.Paths) (Config, error) {
	var c Config
	if p.HostConfig != "" {
		data, err := read(p.HostConfig)
		if err != nil {
			return Config{}, err
		}
		c.HostSpace = space(data)
	}
	c.Space = c.HostSpace
	if p.Config == "" {
		return c, nil
	}
	data, err := read(filepath.Join(p.Config, paths.ConfigFile))
	if err != nil {
		return Config{}, err
	}
	if userSpace := space(data); userSpace != "" {
		c.Space = userSpace
	}
	token, err := read(filepath.Join(p.Config, paths.TokenFile))
	if err != nil {
		return Config{}, err
	}
	c.Token = strings.TrimSuffix(strings.TrimSuffix(string(token), "\n"), "\r")
	return c, nil
}

func read(path string) ([]byte, error) {
	dir, name := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	if name == "" {
		name = "."
	}
	data, err := fs.ReadFile(os.DirFS(dir), name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not read '%s': %s", diagnostic.Text(path), diagnostic.Text(err.Error()))
	}
	return data, nil
}

func space(data []byte) string {
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.Trim(strings.TrimSuffix(line, "\r"), " \t")
		if strings.HasPrefix(line, "space=") {
			value := strings.Trim(strings.TrimPrefix(line, "space="), " \t")
			if ValidSpace(value) {
				return value
			}
			return ""
		}
	}
	return ""
}
