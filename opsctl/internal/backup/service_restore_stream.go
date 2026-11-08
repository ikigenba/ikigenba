package backup

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// The unbuffered pipe couples zstd's output to staging writes. Only metadata
// survives each member; publication renames the very files written here.
func stageServiceRestoreSource(ctx context.Context, env host.Env, client cloud.Client, uri, service string, snapshot bool) (source serviceRestoreSource, err error) {
	object, err := client.GetObject(ctx, uri)
	if err != nil {
		if object != nil {
			err = errors.Join(err, object.Close())
		}
		if snapshot {
			if errors.Is(err, cloud.ErrNotFound) {
				return source, fmt.Errorf("%s: no such object", uri)
			}
			return source, fmt.Errorf("%s: %w", uri, err)
		}
		return source, fmt.Errorf("download %q: %w", uri, err)
	}
	if object == nil {
		return source, fmt.Errorf("download %q: object reader is not configured", uri)
	}
	closeObject := sync.OnceValue(object.Close)
	defer func() { err = errors.Join(err, closeObject()) }()
	stageName, err := os.MkdirTemp(env.Root, ".opsctl-service-restore-")
	if err != nil {
		return source, err
	}
	source.stage = filepath.Base(stageName)
	defer func() {
		if err != nil {
			err = errors.Join(err, os.RemoveAll(stageName))
		}
	}()
	root, err := os.OpenRoot(env.Root)
	if err != nil {
		return source, err
	}
	defer func() { _ = root.Close() }()
	if err := root.Chmod(source.stage, 0o700); err != nil {
		return source, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader, writer := io.Pipe()
	stop := context.AfterFunc(runCtx, func() {
		_ = closeObject()
		_ = reader.CloseWithError(runCtx.Err())
		_ = writer.CloseWithError(runCtx.Err())
	})
	counted := &serviceRestoreCountingReader{Reader: object}
	done := make(chan error, 1)
	go func() {
		result, executeErr := env.Execute(runCtx, host.Command{Name: "zstd", Args: []string{"--quiet", "--decompress", "--stdout"}, Stdin: counted, Stdout: writer})
		if executeErr != nil || result.ExitCode != 0 {
			var commandErr *host.CommandError
			if !errors.As(executeErr, &commandErr) {
				executeErr = &host.CommandError{Label: fmt.Sprintf("decompress %q archive", service), Result: result, Err: executeErr}
			}
		}
		_ = writer.CloseWithError(executeErr)
		done <- executeErr
	}()
	source.entries, err = readServiceRestoreTar(reader, root, source.stage)
	if err != nil {
		_ = reader.CloseWithError(err)
		cancel()
		_ = closeObject()
	}
	executeErr := <-done
	_ = stop()
	_ = reader.Close()
	source.size = counted.size
	if err == nil {
		err = errors.Join(executeErr, ctx.Err())
	}
	err = errors.Join(err, closeObject())
	return source, err
}

type serviceRestoreCountingReader struct {
	io.Reader
	size int64
}

func (r *serviceRestoreCountingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.size += int64(n)
	return n, err
}

type serviceRestoreTarReader struct {
	io.Reader
	size int64
	tail [1024]byte
}

func (r *serviceRestoreTarReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.size += int64(n)
	if n >= len(r.tail) {
		copy(r.tail[:], p[n-len(r.tail):n])
	} else if n > 0 {
		copy(r.tail[:], r.tail[n:])
		copy(r.tail[len(r.tail)-n:], p[:n])
	}
	return n, err
}

type serviceRestoreZeroWriter struct{}

func (serviceRestoreZeroWriter) Write(p []byte) (int, error) {
	if !allZero(p) {
		return 0, errors.New("invalid data after tar archive end")
	}
	return len(p), nil
}

func readServiceRestoreTar(input io.Reader, root *os.Root, stage string) ([]serviceRestoreEntry, error) {
	raw := &serviceRestoreTarReader{Reader: input}
	reader := tar.NewReader(raw)
	entries := make([]serviceRestoreEntry, 0)
	byName := make(map[string]serviceRestoreEntry)
	var buffer [32768]byte
	for {
		before := raw.size
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			if raw.size-before < 1024+(512-before%512)%512 || !allZero(raw.tail[:]) {
				return nil, errors.New("invalid tar archive termination")
			}
			if _, err := io.CopyBuffer(serviceRestoreZeroWriter{}, raw, buffer[:]); err != nil {
				return nil, err
			}
			if raw.size%512 != 0 || !allZero(raw.tail[:]) {
				return nil, errors.New("invalid tar archive termination")
			}
			return entries, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read tar archive: %w", err)
		}
		name, err := validServiceArchiveName(header.Name)
		if err != nil {
			return nil, err
		}
		if name == "etc" || strings.HasPrefix(name, "etc/") {
			if err := copyServiceRestoreMember(io.Discard, reader, buffer[:]); err != nil {
				return nil, err
			}
			continue
		}
		if _, duplicate := byName[name]; duplicate {
			return nil, fmt.Errorf("duplicate archive entry %q", header.Name)
		}
		entry := serviceRestoreEntry{name: name, typeflag: header.Typeflag, mode: restoredFileMode(header.Mode) &^ 0o007, linkname: header.Linkname, uid: header.Uid, gid: header.Gid, uname: header.Uname, gname: header.Gname}
		switch entry.typeflag {
		case tar.TypeReg, byte(0):
			entry.typeflag = tar.TypeReg
		case tar.TypeDir, tar.TypeSymlink:
			if header.Size != 0 {
				return nil, fmt.Errorf("archive entry %q has unexpected data", header.Name)
			}
		case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
			return nil, fmt.Errorf("archive entry %q is a special device", header.Name)
		default:
			return nil, fmt.Errorf("archive entry %q has unsupported type %d", header.Name, header.Typeflag)
		}
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if ancestor, ok := byName[parent]; ok && ancestor.typeflag != tar.TypeDir {
				if ancestor.typeflag == tar.TypeSymlink {
					return nil, fmt.Errorf("archive entry %q traverses symlink %q", name, parent)
				}
				return nil, fmt.Errorf("archive entry %q traverses non-directory %q", name, parent)
			}
		}
		if entry.typeflag != tar.TypeDir {
			for earlier := range byName {
				if strings.HasPrefix(earlier, name+"/") {
					return nil, fmt.Errorf("archive entry %q traverses non-directory %q", earlier, name)
				}
			}
		}
		target := path.Join(stage, name)
		if err := mkdirHostRestoreParents(root, path.Dir(target)); err != nil {
			return nil, err
		}
		switch entry.typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(target, 0o700); err != nil {
				return nil, err
			}
			if err := root.Chmod(target, 0o700); err != nil {
				return nil, err
			}
		case tar.TypeReg:
			file, err := root.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return nil, err
			}
			copyErr := copyServiceRestoreMember(file, reader, buffer[:])
			if err := errors.Join(copyErr, file.Close()); err != nil {
				return nil, fmt.Errorf("write archive entry %q: %w", name, err)
			}
		case tar.TypeSymlink:
			if err := root.Symlink(entry.linkname, target); err != nil {
				return nil, err
			}
		}
		entries = append(entries, entry)
		byName[name] = entry
	}
}

func copyServiceRestoreMember(destination io.Writer, source io.Reader, buffer []byte) error {
	for {
		n, readErr := source.Read(buffer)
		if n > 0 {
			written, writeErr := destination.Write(buffer[:n])
			if writeErr != nil {
				return writeErr
			}
			if written != n {
				return io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}
