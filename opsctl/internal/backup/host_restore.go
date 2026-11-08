package backup

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
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
	Object          string
	Size            int64
	Files           int
	SourceReady     bool
	FilesRestored   bool
	AccountsObject  string
	AccountsSize    int64
	AccountsReady   bool
	Groups          []string
	GroupsRestored  bool
	Users           []string
	Locked          []string
	TimersInstalled bool
	UsersRestored   bool
	HostKeys        int
	SSHRestored     bool
	FailedStep      string
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

	stage, err := newHostRestoreStage(env.Root)
	if err != nil {
		return hostRestoreSourceFailure(result, err)
	}
	defer stage.close()
	result.Size, err = stage.read(ctx, env, client, selected.URI)
	if err != nil {
		return hostRestoreSourceFailure(result, err)
	}
	result.SourceReady = true

	accountsArchive, err := loadRestoreAccounts(ctx, env, client, objects, selected.URI, hostPrefix, &result)
	if err != nil {
		result.FailedStep = "accounts"
		return result, err
	}

	result.Files, err = stage.publish(ctx)
	if err != nil {
		result.FailedStep = "files"
		return result, fmt.Errorf("replace host files: %w", err)
	}
	result.FilesRestored = true
	if accountsArchive != nil {
		if err := replayRestoreAccounts(ctx, env, client, *accountsArchive, &result); err != nil {
			return result, err
		}
	}
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

type hostRestoreEntry struct {
	name     string
	typeflag byte
	mode     fs.FileMode
	linkname string
	data     []byte
	uid      int
	gid      int
	access   string
	defaults string
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
		if err := mkdirHostRestoreParents(root, path.Dir(name)); err != nil {
			return fmt.Errorf("create parent for %q: %w", entry.name, err)
		}
		switch entry.typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, 0o700); err != nil {
				return fmt.Errorf("create directory %q: %w", entry.name, err)
			}
			if err := root.Chmod(name, 0o700); err != nil {
				return err
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

func mkdirHostRestoreParents(root *os.Root, name string) error {
	if name == "." {
		return nil
	}
	if _, err := root.Lstat(name); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := mkdirHostRestoreParents(root, path.Dir(name)); err != nil {
		return err
	}
	if err := root.Mkdir(name, 0o700); err != nil {
		return err
	}
	return root.Chmod(name, 0o700)
}
