package linux

import (
	"bufio"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const releaseverPath = "/etc/dnf/vars/releasever"
const updatesConfig = "/etc/dnf/automatic.conf"

const securityOption = "upgrade_type = security"
const applyOption = "apply_updates = yes"
const commandsSection = "[commands]\n" + securityOption + "\n" + applyOption + "\n"

func (h *Host) updates(ctx context.Context) error {
	if _, err := h.write(releaseverPath, []byte("latest\n"), 0o644); err != nil {
		return err
	}
	if err := h.configureAutomatic(); err != nil {
		return err
	}
	return h.enableAndStart(ctx, "dnf-automatic.timer")
}

func (h *Host) configureAutomatic() error {
	info, err := os.Lstat(h.path(updatesConfig))
	if errors.Is(err, fs.ErrNotExist) {
		_, err := h.write(updatesConfig, []byte(commandsSection), 0o644)
		return err
	}
	if err != nil {
		return hostError(updatesConfig, err)
	}
	if !info.Mode().IsRegular() {
		return hostError(updatesConfig, fs.ErrInvalid)
	}
	input, err := os.Open(h.path(updatesConfig))
	if err != nil {
		return hostError(updatesConfig, err)
	}
	defer func() { _ = input.Close() }()
	return h.writeStream(updatesConfig, info.Mode()&permissionBits, func(output *os.File) (bool, error) {
		return rewriteAutomatic(input, info.Size(), output)
	})
}

// writeStream atomically replaces a file with streamed content when it differs.
func (h *Host) writeStream(path string, mode fs.FileMode, write func(*os.File) (bool, error)) error {
	dest := h.path(path)
	dir := filepath.Dir(dest)
	if err := h.mkdir(filepath.Dir(path)); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".accountsd-")
	if err != nil {
		return hostError(path, err)
	}
	defer func() {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
	}()
	changed, err := write(temp)
	if err != nil {
		return hostError(path, err)
	}
	if !changed {
		h.written[path] = struct{}{}
		return h.mode(path, mode)
	}
	if err := temp.Chmod(mode); err != nil {
		return hostError(path, err)
	}
	if err := temp.Close(); err != nil {
		return hostError(path, err)
	}
	if err := os.Rename(temp.Name(), dest); err != nil {
		return hostError(path, err)
	}
	h.written[path] = struct{}{}
	return nil
}

type configKind uint8

const (
	otherLine configKind = iota
	otherSection
	commandSection
	securityLine
	applyLine
)

type configLine struct {
	size   int64
	ending string
	kind   configKind
}

// Read only the syntax that affects the rewrite. An arbitrarily long comment,
// value or indent is scanned one fragment at a time and copied from disk.
func readConfigLine(reader *bufio.Reader) (configLine, error) {
	var line configLine
	var word [len("apply_updates")]byte
	var first, last, preceding byte
	wordSize := 0
	wordEnded, extraWord, assigned := false, false, false
	for {
		fragment, err := reader.ReadSlice('\n')
		line.size += int64(len(fragment))
		for _, b := range fragment {
			if b == '\n' {
				line.ending = "\n"
				if preceding == '\r' {
					line.ending = "\r\n"
				}
				break
			}
			preceding = b
			if b == ' ' || b == '\t' || b == '\r' {
				wordEnded = wordEnded || wordSize != 0
				continue
			}
			if first == 0 {
				first = b
			}
			last = b
			if assigned {
				continue
			}
			if b == '=' {
				assigned = true
				continue
			}
			if wordEnded || wordSize == len(word) {
				extraWord = true
				continue
			}
			word[wordSize] = b
			wordSize++
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil && (!errors.Is(err, io.EOF) || line.size == 0) {
			return configLine{}, err
		}
		break
	}
	name := string(word[:wordSize])
	switch {
	case first == '[' && last == ']':
		line.kind = otherSection
		if name == "[commands]" && !extraWord && !assigned {
			line.kind = commandSection
		}
	case assigned && !extraWord && name == "upgrade_type":
		line.kind = securityLine
	case assigned && !extraWord && name == "apply_updates":
		line.kind = applyLine
	}
	return line, nil
}

func automaticInsertions(input io.ReaderAt, start, length int64) (string, error) {
	reader := bufio.NewReader(io.NewSectionReader(input, start, length))
	security, apply := false, false
	for {
		line, err := readConfigLine(reader)
		if errors.Is(err, io.EOF) || line.kind == otherSection || line.kind == commandSection {
			break
		}
		if err != nil {
			return "", err
		}
		security = security || line.kind == securityLine
		apply = apply || line.kind == applyLine
		if security && apply {
			return "", nil
		}
	}
	var insert string
	if !security {
		insert += securityOption + "\n"
	}
	if !apply {
		insert += applyOption + "\n"
	}
	return insert, nil
}

func rewriteAutomatic(input io.ReaderAt, size int64, output io.Writer) (bool, error) {
	reader := bufio.NewReader(io.NewSectionReader(input, 0, size))
	var start, pending int64
	seen, inCommands, changed := false, false, false
	ending := "\n"
	for start < size {
		line, err := readConfigLine(reader)
		if err != nil {
			return false, err
		}
		end := start + line.size
		ending = line.ending
		switch {
		case line.kind == commandSection:
			seen, inCommands = true, true
			insert, err := automaticInsertions(input, end, size-end)
			if err != nil {
				return false, err
			}
			if insert != "" {
				if line.ending == "" {
					insert = "\n" + insert
				}
				if err := editConfig(input, output, pending, end, insert); err != nil {
					return false, err
				}
				pending, changed = end, true
			}
		case line.kind == otherSection:
			inCommands = false
		case inCommands && (line.kind == securityLine || line.kind == applyLine):
			option := securityOption
			if line.kind == applyLine {
				option = applyOption
			}
			option += line.ending
			matches, err := equalConfigLine(input, start, line.size, option)
			if err != nil {
				return false, err
			}
			if !matches {
				if err := editConfig(input, output, pending, start, option); err != nil {
					return false, err
				}
				pending, changed = end, true
			}
		}
		start = end
	}
	insert := ""
	if !seen {
		insert = commandsSection
		if ending == "" {
			insert = "\n" + insert
		}
		changed = true
	}
	return changed, editConfig(input, output, pending, size, insert)
}

func equalConfigLine(input io.ReaderAt, start, size int64, option string) (bool, error) {
	if int64(len(option)) != size {
		return false, nil
	}
	var prefix [len(securityOption) + len("\r\n")]byte
	_, err := input.ReadAt(prefix[:len(option)], start)
	return string(prefix[:len(option)]) == option, err
}

func editConfig(input io.ReaderAt, output io.Writer, from, to int64, insert string) error {
	if _, err := io.Copy(output, io.NewSectionReader(input, from, to-from)); err != nil {
		return err
	}
	_, err := io.WriteString(output, insert)
	return err
}
