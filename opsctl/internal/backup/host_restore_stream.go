package backup

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

type hostRestoreStage struct {
	root    *os.Root
	base    string
	entries []hostRestoreEntry
}

func newHostRestoreStage(rootName string) (*hostRestoreStage, error) {
	root, err := os.OpenRoot(rootName)
	if err != nil {
		return nil, err
	}
	name, err := os.MkdirTemp(rootName, ".opsctl-host-restore-")
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	stage := &hostRestoreStage{root: root, base: path.Base(name)}
	if err := root.Chmod(stage.base, 0o700); err != nil {
		stage.close()
		return nil, err
	}
	return stage, nil
}

func (stage *hostRestoreStage) close() {
	// A validated archive can contain directories without owner access. Make
	// staged directories removable even when publication or accounts fails.
	ordered := append([]hostRestoreEntry(nil), stage.entries...)
	sort.Slice(ordered, func(i, j int) bool { return strings.Count(ordered[i].name, "/") < strings.Count(ordered[j].name, "/") })
	for _, entry := range ordered {
		if entry.typeflag == tar.TypeDir {
			_ = stage.root.Chmod(path.Join(stage.base, entry.name), 0o700)
		}
	}
	_ = stage.root.RemoveAll(stage.base)
	_ = stage.root.Close()
}

// A synchronous pipe bounds decompressor output by the consumer's fixed copy
// buffer. Stdin reads the object directly; neither side queues archive bytes.
func (stage *hostRestoreStage) read(ctx context.Context, env host.Env, client cloud.Client, uri string) (int64, error) {
	object, err := client.GetObject(ctx, uri)
	if err != nil {
		if object != nil {
			err = errors.Join(err, object.Close())
		}
		return 0, fmt.Errorf("download %q: %w", uri, err)
	}
	if object == nil {
		return 0, fmt.Errorf("download %q: object reader is not configured", uri)
	}
	closeObject := sync.OnceValue(object.Close)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader, writer := io.Pipe()
	stop := context.AfterFunc(runCtx, func() {
		_ = closeObject()
		_ = reader.CloseWithError(runCtx.Err())
		_ = writer.CloseWithError(runCtx.Err())
	})
	counted := &restoreCountingReader{Reader: object}
	done := make(chan error, 1)
	go func() {
		result, executeErr := env.Execute(runCtx, host.Command{Name: "zstd", Args: []string{"--quiet", "--decompress", "--stdout"}, Stdin: counted, Stdout: writer})
		if executeErr != nil || result.ExitCode != 0 {
			var commandErr *host.CommandError
			if !errors.As(executeErr, &commandErr) {
				executeErr = &host.CommandError{Label: "decompress host archive", Result: result, Err: executeErr}
			}
		}
		_ = writer.CloseWithError(executeErr)
		done <- executeErr
	}()
	parseErr := stage.consume(reader)
	if parseErr != nil {
		_ = reader.CloseWithError(parseErr)
		cancel()
		_ = closeObject()
	}
	executeErr := <-done
	_ = stop()
	_ = reader.Close()
	closeErr := closeObject()
	if parseErr != nil {
		var commandErr *host.CommandError
		if errors.As(parseErr, &commandErr) && executeErr != nil {
			return counted.size, errors.Join(executeErr, closeErr, ctx.Err())
		}
		return counted.size, errors.Join(parseErr, ctx.Err())
	}
	return counted.size, errors.Join(executeErr, closeErr, ctx.Err())
}

func (stage *hostRestoreStage) consume(input io.Reader) error {
	raw := &restoreTarReader{Reader: input}
	reader := tar.NewReader(raw)
	byName := make(map[string]hostRestoreEntry)
	descendants := make(map[string]bool)
	for {
		before := raw.size
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			if raw.size-before < 1024 || !allZero(raw.tail[:]) {
				return errors.New("invalid tar archive termination")
			}
			if _, err := io.Copy(hostRestoreZeros{}, raw); err != nil {
				return err
			}
			if raw.size%512 != 0 || !allZero(raw.tail[:]) {
				return errors.New("invalid tar archive termination")
			}
			return stage.permissions()
		}
		if err != nil {
			return fmt.Errorf("read tar archive: %w", err)
		}
		name, err := validHostArchiveName(header.Name)
		if err != nil {
			return err
		}
		if _, found := byName[name]; found {
			return fmt.Errorf("duplicate archive entry %q", name)
		}
		entry := hostRestoreEntry{name: name, typeflag: header.Typeflag, mode: restoredFileMode(header.Mode), linkname: header.Linkname}
		if name == "etc/ikigenba" {
			entry.mode = 0o700
		} else if strings.HasPrefix(name, "etc/ikigenba/") {
			entry.mode &^= 0o007
		}
		switch entry.typeflag {
		case tar.TypeReg, byte(0):
			entry.typeflag = tar.TypeReg
		case tar.TypeDir, tar.TypeSymlink:
			if header.Size != 0 {
				return fmt.Errorf("archive entry %q has unexpected data", name)
			}
		case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
			return fmt.Errorf("archive entry %q is a special device", name)
		default:
			return fmt.Errorf("archive entry %q has unsupported type %d", name, entry.typeflag)
		}
		if entry.typeflag != tar.TypeDir && descendants[name] {
			return fmt.Errorf("archive entry %q is a non-directory ancestor", name)
		}
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if ancestor, found := byName[parent]; found && ancestor.typeflag != tar.TypeDir {
				if ancestor.typeflag == tar.TypeSymlink {
					return fmt.Errorf("archive entry %q traverses symlink %q", name, parent)
				}
				return fmt.Errorf("archive entry %q traverses non-directory %q", name, parent)
			}
			descendants[parent] = true
		}
		if err := stage.write(entry, reader); err != nil {
			return err
		}
		stage.entries = append(stage.entries, entry)
		byName[name] = entry
	}
}

type hostRestoreZeros struct{}

func (hostRestoreZeros) Write(p []byte) (int, error) {
	if !allZero(p) {
		return 0, errors.New("invalid data after tar archive end")
	}
	return len(p), nil
}

func (stage *hostRestoreStage) write(entry hostRestoreEntry, content io.Reader) error {
	name := path.Join(stage.base, entry.name)
	if err := mkdirHostRestoreParents(stage.root, path.Dir(name)); err != nil {
		return err
	}
	switch entry.typeflag {
	case tar.TypeDir:
		if err := stage.root.MkdirAll(name, 0o700); err != nil {
			return err
		}
		return stage.root.Chmod(name, 0o700)
	case tar.TypeSymlink:
		return stage.root.Symlink(entry.linkname, name)
	case tar.TypeReg:
		file, err := stage.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		var buffer [32768]byte
		_, writeErr := io.CopyBuffer(struct{ io.Writer }{file}, content, buffer[:])
		if err := errors.Join(writeErr, file.Close()); err != nil {
			return fmt.Errorf("write file %q: %w", entry.name, err)
		}
		return stage.root.Chmod(name, entry.mode)
	}
	return nil
}

func (stage *hostRestoreStage) permissions() error {
	ordered := append([]hostRestoreEntry(nil), stage.entries...)
	sort.Slice(ordered, func(i, j int) bool { return strings.Count(ordered[i].name, "/") > strings.Count(ordered[j].name, "/") })
	for _, entry := range ordered {
		if entry.typeflag == tar.TypeDir {
			if err := stage.root.Chmod(path.Join(stage.base, entry.name), entry.mode); err != nil {
				return err
			}
		}
	}
	return nil
}

func (stage *hostRestoreStage) publish(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	info, err := stage.root.Lstat("etc")
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := stage.root.Mkdir("etc", 0o755); err != nil {
			return 0, err
		}
	case err != nil:
		return 0, err
	case !info.IsDir() || info.Mode()&os.ModeSymlink != 0:
		return 0, errors.New("destination parent /etc is not a directory")
	}
	count := 0
	for _, tree := range []string{"etc/ikigenba", "etc/letsencrypt"} {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		if err := stage.root.RemoveAll(tree); err != nil {
			return count, err
		}
		if hostArchiveContainsTree(stage.entries, tree) {
			if err := stage.root.Rename(path.Join(stage.base, tree), tree); err != nil {
				return count, err
			}
		}
		count += hostTreeFileCount(stage.entries, tree)
	}
	return count, nil
}
