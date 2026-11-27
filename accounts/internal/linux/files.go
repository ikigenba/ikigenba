package linux

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
)

type fileChange struct {
	changed bool
	backup  string
}

func (h *Host) path(hostPath string) string { return filepath.Join(h.env.Root, hostPath) }

func hostError(path string, err error) error {
	return fmt.Errorf("%s: %w", path, err)
}

func (h *Host) read(hostPath string) ([]byte, error) {
	data, err := os.ReadFile(h.path(hostPath))
	if err != nil {
		return nil, hostError(hostPath, err)
	}
	return data, nil
}

func (h *Host) mkdir(hostPath string) error {
	path := h.path(hostPath)
	if err := os.Mkdir(path, 0o700); err != nil {
		if errors.Is(err, fs.ErrExist) {
			info, statErr := os.Stat(path)
			if statErr == nil && info.IsDir() {
				return nil
			}
		}
		if !errors.Is(err, fs.ErrNotExist) || filepath.Dir(hostPath) == hostPath {
			return hostError(hostPath, err)
		}
		if err := h.mkdir(filepath.Dir(hostPath)); err != nil {
			return err
		}
		if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return hostError(hostPath, err)
		}
	}
	return h.mode(hostPath, 0o755)
}

// readBounded reads a protected state file without following its final entry.
func (h *Host) readBounded(path string) ([]byte, error) {
	full := h.path(path)
	fd, err := unix.Open(full, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, hostError(path, err)
	}
	f := os.NewFile(uintptr(fd), full)
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, hostError(path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, hostError(path, fs.ErrInvalid)
	}
	if info.Size() > MaxCapture {
		return nil, hostError(path, machine.ErrTooLarge)
	}
	value, err := io.ReadAll(io.LimitReader(f, MaxCapture))
	if err != nil {
		return nil, hostError(path, err)
	}
	return value, nil
}

// writeAtomic replaces a state file without touching Apply's isolation tracking.
func (h *Host) writeAtomic(path string, data []byte, mode fs.FileMode) (bool, error) {
	full := h.path(path)
	if same, _ := sameContents(full, data); same {
		return false, h.atomicFileMode(path, mode)
	}
	temp, err := h.stageFile(path, data, mode)
	if err != nil {
		return false, err
	}
	defer func() { _ = os.Remove(temp) }()
	if err := os.Rename(temp, full); err != nil {
		return false, hostError(path, err)
	}
	return true, nil
}

func (h *Host) atomicFileMode(path string, mode fs.FileMode) error {
	full := h.path(path)
	info, err := os.Lstat(full)
	if err != nil {
		return hostError(path, err)
	}
	if !info.Mode().IsRegular() {
		return hostError(path, fs.ErrInvalid)
	}
	if info.Mode()&permissionBits == mode {
		return nil
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink > 1 {
		// An in-place chmod could change an unrelated path sharing this inode.
		return hostError(path, fs.ErrInvalid)
	}
	if err := os.Chmod(full, mode); err != nil {
		return hostError(path, err)
	}
	return nil
}

func (h *Host) stageFile(path string, data []byte, mode fs.FileMode) (string, error) {
	return h.stageFileWithWriter(path, mode, func(file *os.File) error { return writeFileBytes(file, data) })
}

func (h *Host) stageFileWithWriter(path string, mode fs.FileMode, write func(*os.File) error) (string, error) {
	if err := h.mkdir(filepath.Dir(path)); err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(filepath.Dir(h.path(path)), ".accountsd-")
	if err != nil {
		return "", hostError(path, err)
	}
	if err := errors.Join(temp.Chmod(mode), write(temp), temp.Close()); err != nil {
		_ = os.Remove(temp.Name())
		return "", hostError(path, err)
	}
	return temp.Name(), nil
}

func writeFileBytes(file *os.File, data []byte) error {
	_, err := file.Write(data)
	return err
}

func (h *Host) dir(hostPath string, mode fs.FileMode) (bool, error) {
	path := h.path(hostPath)
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := h.mkdir(hostPath); err != nil {
			return false, err
		}
		return true, h.mode(hostPath, mode)
	}
	if err != nil {
		return false, hostError(hostPath, err)
	}
	if !info.IsDir() {
		return false, hostError(hostPath, fs.ErrInvalid)
	}
	return false, h.mode(hostPath, mode)
}

const permissionBits = fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky

func (h *Host) mode(hostPath string, mode fs.FileMode) error {
	path := h.path(hostPath)
	info, err := os.Lstat(path)
	if err != nil {
		return hostError(hostPath, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return hostError(hostPath, fs.ErrInvalid)
	}
	if info.Mode()&permissionBits != mode {
		if err := os.Chmod(path, mode); err != nil {
			return hostError(hostPath, err)
		}
	}
	return nil
}

func sameContents(path string, expected []byte) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fs.ErrInvalid
	}
	if info.Size() != int64(len(expected)) {
		return false, nil
	}
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	var buffer [4096]byte
	for len(expected) > 0 {
		count := min(len(expected), len(buffer))
		if _, err := io.ReadFull(f, buffer[:count]); err != nil {
			return false, err
		}
		if !bytes.Equal(buffer[:count], expected[:count]) {
			return false, nil
		}
		expected = expected[count:]
	}
	return true, nil
}

func (h *Host) write(hostPath string, data []byte, mode fs.FileMode) (fileChange, error) {
	full := h.path(hostPath)
	equal, err := sameContents(full, data)
	if equal {
		if err := h.mode(hostPath, mode); err != nil {
			return fileChange{}, err
		}
		h.written[hostPath] = struct{}{}
		return fileChange{}, nil
	}
	missing := errors.Is(err, fs.ErrNotExist)
	if err != nil && !missing {
		return fileChange{}, hostError(hostPath, err)
	}
	if err := h.mkdir(filepath.Dir(hostPath)); err != nil {
		return fileChange{}, err
	}
	return h.replace(hostPath, full, data, mode, !missing)
}

func (h *Host) replace(hostPath, full string, data []byte, mode fs.FileMode, existing bool) (fileChange, error) {
	tmp, err := os.CreateTemp(filepath.Dir(full), ".accountsd-")
	if err != nil {
		return fileChange{}, hostError(hostPath, err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	if err := tmp.Chmod(mode); err != nil {
		return fileChange{}, hostError(hostPath, err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fileChange{}, hostError(hostPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fileChange{}, hostError(hostPath, err)
	}
	change := fileChange{changed: true}
	if existing {
		change.backup = tmp.Name() + ".previous"
		if err := os.Link(full, change.backup); err != nil {
			return fileChange{}, hostError(hostPath, err)
		}
		h.backups = append(h.backups, change.backup)
	}
	if err := os.Rename(tmp.Name(), full); err != nil {
		return fileChange{}, hostError(hostPath, err)
	}
	h.written[hostPath] = struct{}{}
	return change, nil
}

func (h *Host) restore(hostPath string, change fileChange) error {
	if !change.changed {
		return nil
	}
	if change.backup == "" {
		_, err := h.remove(hostPath)
		return err
	}
	if err := os.Rename(change.backup, h.path(hostPath)); err != nil {
		return hostError(hostPath, err)
	}
	return nil
}

func (h *Host) clearBackups() {
	for _, path := range h.backups {
		_ = os.Remove(path)
	}
	h.backups = h.backups[:0]
}

func (h *Host) remove(hostPath string) (bool, error) {
	err := os.Remove(h.path(hostPath))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, hostError(hostPath, err)
	}
	return true, nil
}

// lineWriter streams output, discarding individual lines beyond the chosen limit.
type lineWriter struct {
	line          []byte
	overLimit     bool
	limit         int
	emit          func([]byte)
	countNonempty bool
	nonempty      bool
	total         int
}

func (w *lineWriter) Write(p []byte) (int, error) {
	size := len(p)
	for len(p) > 0 {
		part, next, found := bytes.Cut(p, []byte{'\n'})
		if w.countNonempty && !w.nonempty {
			w.nonempty = len(bytes.Trim(part, " \t\r")) != 0
		}
		if !w.overLimit {
			if len(w.line)+len(part) > w.limit {
				w.overLimit = true
			} else {
				w.line = append(w.line, part...)
			}
		}
		if found {
			w.finish()
		}
		p = next
	}
	return size, nil
}

func (w *lineWriter) finish() {
	if !w.overLimit {
		w.emit(w.line)
	}
	w.line = w.line[:0]
	w.overLimit = false
	if w.nonempty {
		w.total++
		w.nonempty = false
	}
}

func (h *Host) scan(hostPath string, visit func(string)) error {
	f, err := os.Open(h.path(hostPath))
	if err != nil {
		return hostError(hostPath, err)
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		visit(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return hostError(hostPath, err)
	}
	return nil
}
