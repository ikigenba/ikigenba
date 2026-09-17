package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// HostRestoreResult describes the completed phases of a host restore.
type HostRestoreResult struct {
	Object        string
	Size          int64
	Files         int
	SourceReady   bool
	FilesRestored bool
	FailedStep    string
}

// HostRestore replaces the host configuration and certificate trees from the
// newest validly named host backup.
func HostRestore(ctx context.Context, env host.Env, cloudEnv cloud.Env, store config.Store) (HostRestoreResult, error) {
	prefix, region, err := fileBackupConfiguration(store, "")
	if err != nil {
		return HostRestoreResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return HostRestoreResult{}, fmt.Errorf("restore host: %w", err)
	}
	if env.Execute == nil {
		return HostRestoreResult{}, errors.New("restore host: host execution is not configured")
	}
	if cloudEnv.Open == nil {
		return HostRestoreResult{}, errors.New("restore host: cloud access is not configured")
	}
	client, err := cloudEnv.Open(ctx, region)
	if err != nil {
		return HostRestoreResult{}, fmt.Errorf("open backup storage: %w", err)
	}
	if client == nil {
		return HostRestoreResult{}, errors.New("open backup storage: cloud client is not configured")
	}
	if err := ctx.Err(); err != nil {
		return HostRestoreResult{}, fmt.Errorf("restore host: %w", err)
	}

	result := HostRestoreResult{}
	hostPrefix := strings.TrimSuffix(prefix, "/") + "/host/"
	objects, err := client.ListObjects(ctx, hostPrefix)
	if err != nil {
		return hostRestoreSourceFailure(result, fmt.Errorf("list host backups: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return hostRestoreSourceFailure(result, fmt.Errorf("list host backups: %w", err))
	}
	selected, found := newestHostArchive(objects, hostPrefix)
	if !found {
		return hostRestoreSourceFailure(result, errors.New("no host backup found"))
	}
	basename := strings.TrimPrefix(selected.URI, hostPrefix)
	result.Object = "host/" + basename

	reader, getErr := client.GetObject(ctx, selected.URI)
	if getErr != nil {
		if reader != nil {
			getErr = errors.Join(getErr, reader.Close())
		}
		return hostRestoreSourceFailure(result, fmt.Errorf("download %q: %w", selected.URI, getErr))
	}
	if reader == nil {
		return hostRestoreSourceFailure(result, fmt.Errorf("download %q: object reader is not configured", selected.URI))
	}
	compressed, readErr := io.ReadAll(reader)
	result.Size = int64(len(compressed))
	closeErr := reader.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return hostRestoreSourceFailure(result, fmt.Errorf("download %q: %w", selected.URI, err))
	}
	if err := ctx.Err(); err != nil {
		return hostRestoreSourceFailure(result, fmt.Errorf("download %q: %w", selected.URI, err))
	}

	archive, err := decompressHostArchive(ctx, env.Execute, compressed)
	if err != nil {
		return hostRestoreSourceFailure(result, err)
	}
	entries, err := validateHostArchive(archive)
	if err != nil {
		return hostRestoreSourceFailure(result, fmt.Errorf("validate %q: %w", selected.URI, err))
	}
	if err := ctx.Err(); err != nil {
		return hostRestoreSourceFailure(result, fmt.Errorf("validate %q: %w", selected.URI, err))
	}
	result.SourceReady = true

	result.Files, err = replaceHostTrees(ctx, env.Root, entries)
	if err != nil {
		result.FailedStep = "files"
		return result, fmt.Errorf("replace host files: %w", err)
	}
	result.FilesRestored = true
	return result, nil
}

func hostRestoreSourceFailure(result HostRestoreResult, err error) (HostRestoreResult, error) {
	result.FailedStep = "source"
	return result, err
}

func newestHostArchive(objects []cloud.Object, prefix string) (cloud.Object, bool) {
	var selected cloud.Object
	var selectedTime time.Time
	found := false
	for _, object := range objects {
		if !strings.HasPrefix(object.URI, prefix) {
			continue
		}
		basename := strings.TrimPrefix(object.URI, prefix)
		if basename == "" || strings.Contains(basename, "/") || !strings.HasSuffix(basename, ".tar.zst") {
			continue
		}
		stamp := strings.TrimSuffix(basename, ".tar.zst")
		parsed, err := time.Parse(time.RFC3339, stamp)
		if err != nil {
			continue
		}
		if !found || parsed.After(selectedTime) || parsed.Equal(selectedTime) && object.URI < selected.URI {
			selected = object
			selectedTime = parsed
			found = true
		}
	}
	return selected, found
}

func decompressHostArchive(ctx context.Context, execute func(context.Context, host.Command) (host.Result, error), compressed []byte) ([]byte, error) {
	result, err := execute(ctx, host.Command{
		Name:  "zstd",
		Args:  []string{"--quiet", "--decompress", "--stdout"},
		Stdin: bytes.NewReader(compressed),
	})
	const label = "decompress host archive"
	if err != nil {
		var commandErr *host.CommandError
		if errors.As(err, &commandErr) {
			return nil, err
		}
		return nil, &host.CommandError{Label: label, Result: result, Err: err}
	}
	if result.ExitCode != 0 {
		return nil, &host.CommandError{Label: label, Result: result}
	}
	return result.Stdout, nil
}

type hostRestoreEntry struct {
	name     string
	typeflag byte
	mode     fs.FileMode
	linkname string
	data     []byte
}

func validateHostArchive(archive []byte) ([]hostRestoreEntry, error) {
	if len(archive) < 1024 || len(archive)%512 != 0 || !allZero(archive[len(archive)-1024:]) {
		return nil, errors.New("invalid tar archive termination")
	}
	raw := bytes.NewReader(archive)
	reader := tar.NewReader(raw)
	entries := make([]hostRestoreEntry, 0)
	byName := make(map[string]hostRestoreEntry)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			remaining := archive[len(archive)-raw.Len():]
			if !allZero(remaining) {
				return nil, errors.New("invalid data after tar archive end")
			}
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar archive: %w", err)
		}
		name, err := validHostArchiveName(header.Name)
		if err != nil {
			return nil, err
		}
		if _, duplicate := byName[name]; duplicate {
			return nil, fmt.Errorf("duplicate archive entry %q", header.Name)
		}
		entry := hostRestoreEntry{name: name, typeflag: header.Typeflag, mode: restoredFileMode(header.Mode), linkname: header.Linkname}
		switch header.Typeflag {
		case tar.TypeReg, byte(0):
			entry.typeflag = tar.TypeReg
			entry.data, err = io.ReadAll(reader)
			if err != nil {
				return nil, fmt.Errorf("read archive entry %q: %w", header.Name, err)
			}
		case tar.TypeDir, tar.TypeSymlink:
			if header.Size != 0 {
				return nil, fmt.Errorf("archive entry %q has unexpected data", header.Name)
			}
		case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
			return nil, fmt.Errorf("archive entry %q is a special device", header.Name)
		default:
			return nil, fmt.Errorf("archive entry %q has unsupported type %d", header.Name, header.Typeflag)
		}
		entries = append(entries, entry)
		byName[name] = entry
	}

	for _, entry := range entries {
		for parent := path.Dir(entry.name); parent != "."; parent = path.Dir(parent) {
			ancestor, ok := byName[parent]
			if !ok {
				continue
			}
			if ancestor.typeflag == tar.TypeSymlink {
				return nil, fmt.Errorf("archive entry %q traverses symlink %q", entry.name, parent)
			}
			if ancestor.typeflag != tar.TypeDir {
				return nil, fmt.Errorf("archive entry %q traverses non-directory %q", entry.name, parent)
			}
		}
	}
	return entries, nil
}

func allZero(data []byte) bool {
	for _, value := range data {
		if value != 0 {
			return false
		}
	}
	return true
}

func validHostArchiveName(headerName string) (string, error) {
	if headerName == "" || path.IsAbs(headerName) {
		return "", fmt.Errorf("unsafe archive entry %q", headerName)
	}
	name := strings.TrimSuffix(headerName, "/")
	if name == "" || path.Clean(name) != name {
		return "", fmt.Errorf("unsafe archive entry %q", headerName)
	}
	for _, component := range strings.Split(name, "/") {
		if component == ".." || component == "." || component == "" {
			return "", fmt.Errorf("unsafe archive entry %q", headerName)
		}
	}
	if name != "etc/ikigenba" && !strings.HasPrefix(name, "etc/ikigenba/") &&
		name != "etc/letsencrypt" && !strings.HasPrefix(name, "etc/letsencrypt/") {
		return "", fmt.Errorf("archive entry %q is outside host restore trees", headerName)
	}
	return name, nil
}

func restoredFileMode(mode int64) fs.FileMode {
	restored := fs.FileMode(mode & 0o777)
	if mode&0o4000 != 0 {
		restored |= fs.ModeSetuid
	}
	if mode&0o2000 != 0 {
		restored |= fs.ModeSetgid
	}
	if mode&0o1000 != 0 {
		restored |= fs.ModeSticky
	}
	return restored
}

func replaceHostTrees(ctx context.Context, rootName string, entries []hostRestoreEntry) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	root, err := os.OpenRoot(rootName)
	if err != nil {
		return 0, err
	}
	defer func() { _ = root.Close() }()

	stageName, err := os.MkdirTemp(rootName, ".opsctl-host-restore-")
	if err != nil {
		return 0, err
	}
	stageBase := path.Base(stageName)
	defer func() { _ = root.RemoveAll(stageBase) }()
	if err := root.MkdirAll(path.Join(stageBase, "etc"), 0o700); err != nil {
		return 0, err
	}
	if err := populateHostRestoreStage(root, stageBase, entries); err != nil {
		return 0, err
	}
	etcInfo, err := root.Lstat("etc")
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := root.Mkdir("etc", 0o755); err != nil {
			return 0, err
		}
	case err != nil:
		return 0, err
	case !etcInfo.IsDir() || etcInfo.Mode()&os.ModeSymlink != 0:
		return 0, errors.New("destination parent /etc is not a directory")
	}

	restored := 0
	for _, tree := range []string{"etc/ikigenba", "etc/letsencrypt"} {
		if err := ctx.Err(); err != nil {
			return restored, err
		}
		if err := root.RemoveAll(tree); err != nil {
			return restored, err
		}
		if hostArchiveContainsTree(entries, tree) {
			if err := root.Rename(path.Join(stageBase, tree), tree); err != nil {
				return restored, err
			}
		}
		restored += hostTreeFileCount(entries, tree)
	}
	return restored, nil
}

func populateHostRestoreStage(root *os.Root, stageBase string, entries []hostRestoreEntry) error {
	ordered := append([]hostRestoreEntry(nil), entries...)
	sort.Slice(ordered, func(i, j int) bool {
		leftDepth := strings.Count(ordered[i].name, "/")
		rightDepth := strings.Count(ordered[j].name, "/")
		if leftDepth != rightDepth {
			return leftDepth < rightDepth
		}
		if ordered[i].typeflag != ordered[j].typeflag {
			return ordered[i].typeflag == tar.TypeDir
		}
		return ordered[i].name < ordered[j].name
	})

	directories := make([]hostRestoreEntry, 0)
	for _, entry := range ordered {
		name := path.Join(stageBase, entry.name)
		if err := root.MkdirAll(path.Dir(name), 0o700); err != nil {
			return fmt.Errorf("create parent for %q: %w", entry.name, err)
		}
		switch entry.typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, 0o700); err != nil {
				return fmt.Errorf("create directory %q: %w", entry.name, err)
			}
			directories = append(directories, entry)
		case tar.TypeReg:
			file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return fmt.Errorf("create file %q: %w", entry.name, err)
			}
			_, writeErr := file.Write(entry.data)
			closeErr := file.Close()
			if err := errors.Join(writeErr, closeErr); err != nil {
				return fmt.Errorf("write file %q: %w", entry.name, err)
			}
			if err := root.Chmod(name, entry.mode); err != nil {
				return fmt.Errorf("set permissions on %q: %w", entry.name, err)
			}
		case tar.TypeSymlink:
			if err := root.Symlink(entry.linkname, name); err != nil {
				return fmt.Errorf("create symlink %q: %w", entry.name, err)
			}
		default:
			panic("validated host restore entry has unexpected type " + strconv.Itoa(int(entry.typeflag)))
		}
	}
	for index := len(directories) - 1; index >= 0; index-- {
		entry := directories[index]
		if err := root.Chmod(path.Join(stageBase, entry.name), entry.mode); err != nil {
			return fmt.Errorf("set permissions on %q: %w", entry.name, err)
		}
	}
	return nil
}

func hostArchiveContainsTree(entries []hostRestoreEntry, tree string) bool {
	for _, entry := range entries {
		if entry.name == tree || strings.HasPrefix(entry.name, tree+"/") {
			return true
		}
	}
	return false
}

func hostTreeFileCount(entries []hostRestoreEntry, tree string) int {
	count := 0
	for _, entry := range entries {
		if entry.name != tree && !strings.HasPrefix(entry.name, tree+"/") {
			continue
		}
		if entry.typeflag == tar.TypeReg || entry.typeflag == tar.TypeSymlink {
			count++
		}
	}
	return count
}
