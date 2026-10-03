package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// namedConfig replaces only the effective last name value. The surrounding
// spelling, other assignments, comments and whitespace remain byte-for-byte.
func namedConfig(config []byte, name string) ([]byte, error) {
	section := false
	first, last := -1, -1
	replacement := name
	for start := 0; start < len(config); {
		end := start
		quoted, comment := false, false
		for end < len(config) {
			if !comment && config[end] == '\\' && end+1 < len(config) && config[end+1] == '\n' {
				end += 2
				continue
			}
			if !comment && config[end] == '\\' && end+2 < len(config) && config[end+1] == '\r' && config[end+2] == '\n' {
				end += 3
				continue
			}
			if config[end] == '\n' {
				break
			}
			if !comment && config[end] == '\\' && end+1 < len(config) {
				end += 2
				continue
			}
			if !comment && config[end] == '"' {
				quoted = !quoted
			}
			if !quoted && (config[end] == '#' || config[end] == ';') {
				comment = true
			}
			end++
		}
		p := start
		if p == 0 && bytes.HasPrefix(config, []byte{0xef, 0xbb, 0xbf}) {
			p += 3
		}
		for p < end && configSpace(config[p]) {
			p++
		}
		if p < end && config[p] == '[' {
			sectionEnd := bytes.IndexByte(config[p:end], ']')
			if sectionEnd < 0 {
				return nil, errors.New("invalid repository config section")
			}
			section = strings.EqualFold(strings.TrimSpace(string(config[p+1:p+sectionEnd])), "ikigenba")
			p += sectionEnd + 1
			for p < end && configSpace(config[p]) {
				p++
			}
		}
		if section && p < end && config[p] != '#' && config[p] != ';' {
			keyStart := p
			for p < end && (config[p] >= 'a' && config[p] <= 'z' || config[p] >= 'A' && config[p] <= 'Z' || config[p] >= '0' && config[p] <= '9' || config[p] == '-') {
				p++
			}
			if strings.EqualFold(string(config[keyStart:p]), "name") {
				keyEnd := p
				for p < end && configSpace(config[p]) {
					p++
				}
				if p == end || config[p] == '#' || config[p] == ';' {
					first, last = keyEnd, keyEnd
					replacement = " = " + name
				} else if config[p] == '=' {
					p++
					for p < end && configSpace(config[p]) {
						p++
					}
					valueStart := p
					quoted := false
					for p < end {
						if config[p] == '\\' && p+1 < end {
							p += 2
							continue
						}
						if config[p] == '"' {
							quoted = !quoted
						}
						if !quoted && (config[p] == '#' || config[p] == ';') {
							break
						}
						p++
					}
					for p > valueStart && configSpace(config[p-1]) {
						p--
					}
					first, last = valueStart, p
					replacement = name
					// Preserve a value's enclosing quotes as part of its original spelling.
					if last-first >= 2 && config[first] == '"' && config[last-1] == '"' {
						first++
						last--
					}
				}
			}
		}
		start = end + 1
	}
	if first < 0 {
		return nil, errors.New("repository config has no name")
	}
	result := make([]byte, 0, len(config)+len(replacement)-(last-first))
	result = append(result, config[:first]...)
	result = append(result, replacement...)
	result = append(result, config[last:]...)
	return result, nil
}

// writeNameConfig uses git to validate the edited identity before an atomic
// config replacement, including the same directory-write check git's lock uses.
func (s *Store) writeNameConfig(ctx context.Context, root *os.Root, relative string, config []byte, mode os.FileMode, name string) error {
	return replaceConfig(root, relative, config, mode, func(lock string) error {
		path, err := filepath.Abs(filepath.Join(s.cfg.Root, lock))
		if err != nil {
			return err
		}
		value, err := s.cfg.Git.Output(ctx, "/", "config", "--file", path, "--get", "ikigenba.name")
		if err != nil {
			return err
		}
		if string(value) != name+"\n" {
			return errors.New("repository config name update failed")
		}
		return ctx.Err()
	})
}

// replaceConfig installs bytes through a fresh lock descriptor, so restoring a
// readable read-only config does not require reopening that config for writing.
func replaceConfig(root *os.Root, relative string, config []byte, mode os.FileMode, validate func(string) error) error {
	lock := relative + ".lock"
	file, err := root.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(lock) }()
	if _, err = file.Write(config); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = root.Chmod(lock, mode); err != nil {
		return err
	}
	if validate != nil {
		if err = validate(lock); err != nil {
			return err
		}
	}
	return root.Rename(lock, relative)
}

func configSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\v' || value == '\f'
}
