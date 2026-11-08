package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/ikigenba/ikigenba/opsctl/internal/accounts"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/hardening"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// streamRestoreAccounts couples zstd's writes to the work consuming the tar.
// The pipe has no queue: a write can get at most one copy buffer ahead of
// filesystem writes or the completion of the preceding tree.
func streamRestoreAccounts(ctx context.Context, env host.Env, client cloud.Client, uri string, expected []byte, visit func(hostRestoreEntry, io.Reader) error, finish func() error) (int64, error) {
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
			executeErr = &host.CommandError{Label: "decompress host archive", Result: result, Err: executeErr}
		}
		_ = writer.CloseWithError(executeErr)
		done <- executeErr
	}()
	parseErr := readRestoreAccountsTar(reader, expected, visit, finish)
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
		return counted.size, parseErr
	}
	if err := errors.Join(executeErr, closeErr, ctx.Err()); err != nil {
		return counted.size, err
	}
	return counted.size, nil
}

type restoreCountingReader struct {
	io.Reader
	size int64
}

func (r *restoreCountingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.size += int64(n)
	return n, err
}

// tar.Reader accepts missing end markers, so track the raw stream as well as
// its parsed members, and validate the zero trailer while consuming to EOF.
type restoreTarReader struct {
	io.Reader
	size int64
	tail [1024]byte
}

func (r *restoreTarReader) Read(p []byte) (int, error) {
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

type restoreZeroWriter struct{}

func (restoreZeroWriter) Write(p []byte) (int, error) {
	if !allZero(p) {
		return 0, errors.New("invalid data after accounts tar archive end")
	}
	return len(p), nil
}

func readRestoreAccountsTar(input io.Reader, expected []byte, visit func(hostRestoreEntry, io.Reader) error, finish func() error) error {
	raw := &restoreTarReader{Reader: input}
	reader := tar.NewReader(raw)
	byName := make(map[string]hostRestoreEntry)
	var manifest accounts.Manifest
	category, previousKey := 0, ""
	first := true
	for {
		before := raw.size
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			// Next must have consumed both zero blocks, rather than accepting a
			// bare EOF after a member or one zero block.
			if raw.size-before < 1024 {
				return errors.New("invalid accounts tar archive termination")
			}
			if first || !allZero(raw.tail[:]) {
				return errors.New("invalid accounts tar archive termination")
			}
			if finish != nil {
				if err := finish(); err != nil {
					return err
				}
			}
			if _, err := io.Copy(restoreZeroWriter{}, raw); err != nil {
				return err
			}
			if first || raw.size < 1024 || raw.size%512 != 0 || !allZero(raw.tail[:]) {
				return errors.New("invalid accounts tar archive termination")
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("read accounts tar archive: %w", err)
		}
		name := strings.TrimSuffix(header.Name, "/")
		if name == "" || path.IsAbs(name) || path.Clean(name) != name || strings.Contains(name, "//") {
			return fmt.Errorf("unsafe accounts archive entry %q", header.Name)
		}
		for component := range strings.SplitSeq(name, "/") {
			if component == "" || component == "." || component == ".." {
				return fmt.Errorf("unsafe accounts archive entry %q", header.Name)
			}
		}
		if _, found := byName[name]; found {
			return fmt.Errorf("duplicate accounts archive entry %q", name)
		}
		entry := hostRestoreEntry{name: name, typeflag: header.Typeflag, mode: restoredFileMode(header.Mode), linkname: header.Linkname, uid: header.Uid, gid: header.Gid, access: header.PAXRecords["SCHILY.acl.access"], defaults: header.PAXRecords["SCHILY.acl.default"]}
		switch entry.typeflag {
		case tar.TypeReg, byte(0):
			entry.typeflag = tar.TypeReg
		case tar.TypeDir, tar.TypeSymlink:
			if header.Size != 0 {
				return fmt.Errorf("accounts archive entry %q has unexpected data", name)
			}
		default:
			return fmt.Errorf("accounts archive entry %q has unsupported type", name)
		}
		for _, key := range []string{"SCHILY.acl.access", "SCHILY.acl.default"} {
			if value, found := header.PAXRecords[key]; found && !validRestoreACL(value) {
				return fmt.Errorf("invalid %s in %q", key, name)
			}
		}
		if first {
			if name != "accounts.json" || entry.typeflag != tar.TypeReg {
				return errors.New("first accounts member must be regular accounts.json")
			}
			data, err := io.ReadAll(reader)
			if err != nil {
				return err
			}
			if expected != nil && !bytes.Equal(data, expected) {
				return errors.New("accounts.json changed since validation")
			}
			if err := decodeRestoreManifest(data, &manifest); err != nil {
				return err
			}
			if err := visit(entry, bytes.NewReader(data)); err != nil {
				return err
			}
			first = false
		} else {
			if err := validRestoreAccountsEntry(entry, manifest, byName); err != nil {
				return err
			}
			nextCategory, key := restoreAccountsOrder(entry.name)
			if nextCategory < category || nextCategory == category && key < previousKey || nextCategory == category && key == previousKey && nextCategory < 3 {
				return fmt.Errorf("accounts archive entry %q is out of order", name)
			}
			category, previousKey = nextCategory, key
			if err := visit(entry, reader); err != nil {
				return err
			}
		}
		byName[name] = entry
		if err := discardRestoreMember(reader); err != nil {
			return fmt.Errorf("read accounts archive entry %q: %w", name, err)
		}
	}
}

func restoreAccountsOrder(name string) (int, string) {
	parts := strings.Split(name, "/")
	switch parts[0] {
	case "home":
		return 3, parts[1]
	case "srv":
		return 4, parts[1]
	}
	if strings.HasPrefix(name, "etc/ssh/authorized_keys/") {
		return 2, parts[len(parts)-1]
	}
	return 1, name
}

func validateRestoreManifest(manifest accounts.Manifest) error {
	names, uids, gids := map[string]bool{}, map[int]bool{}, map[int]bool{}
	groups := map[string]bool{}
	add := func(name string, uid, gid int) error {
		if !accounts.ValidName(name) || names[name] || uid < 0 || gid < 0 || uids[uid] || gids[gid] {
			return errors.New("invalid accounts manifest names or ids")
		}
		names[name], uids[uid], gids[gid] = true, true, true
		return nil
	}
	previous := ""
	for _, group := range manifest.Groups {
		if group.Slug <= previous {
			return errors.New("accounts manifest groups are out of order")
		}
		if err := add(group.Slug, group.UID, group.GID); err != nil {
			return err
		}
		for _, member := range group.Members {
			if !accounts.ValidName(member) {
				return errors.New("invalid accounts manifest member")
			}
		}
		groups[group.Slug], previous = true, group.Slug
	}
	previous = ""
	for _, user := range manifest.Users {
		if user.Name <= previous {
			return errors.New("accounts manifest users are out of order")
		}
		if err := add(user.Name, user.UID, user.GID); err != nil {
			return err
		}
		if user.State != accounts.StateActive && user.State != accounts.StateLocked {
			return errors.New("invalid accounts manifest state")
		}
		seen := map[string]bool{}
		for _, group := range user.Groups {
			if !groups[group] || seen[group] {
				return errors.New("invalid accounts manifest user group")
			}
			seen[group] = true
		}
		previous = user.Name
	}
	return nil
}

func replayRestoreAccountStep(ctx context.Context, env host.Env, client cloud.Client, archive restoreAccountsArchive, result *HostRestoreResult, users bool) error {
	root, err := os.OpenRoot(env.Root)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	var stage, tree string
	var members []hostRestoreEntry
	defer func() {
		if stage != "" {
			_ = root.RemoveAll(stage)
		}
	}()
	index := 0
	names := make([]string, 0)
	prefix := "srv/"
	if users {
		prefix = "home/"
		for _, user := range archive.manifest.Users {
			names = append(names, user.Name)
		}
	} else {
		for _, group := range archive.manifest.Groups {
			names = append(names, group.Slug)
		}
	}
	complete := func(entries []hostRestoreEntry) error {
		name := names[index]
		if users {
			key := strings.TrimPrefix(hardening.KeysDir, "/") + "/" + name
			keys := slices.DeleteFunc(slices.Clone(archive.entries), func(e hostRestoreEntry) bool { return e.name != key })
			if err := replaceRestoreAccountsMembers(ctx, env, keys); err != nil {
				return err
			}
			entries = append(keys, entries...)
		} else if err := prepareRestoreAccountsParent(env.Root, "srv"); err != nil {
			return err
		}
		if err := restoreAccountsMetadata(ctx, env, entries); err != nil {
			return err
		}
		if users {
			user := archive.manifest.Users[index]
			if err := accounts.RestoreUser(ctx, env, user); err != nil {
				return err
			}
			result.Users = append(result.Users, name)
			if user.State == accounts.StateLocked {
				result.Locked = append(result.Locked, name)
			}
		} else {
			if err := accounts.RestoreGroup(ctx, env, archive.manifest.Groups[index]); err != nil {
				return err
			}
			result.Groups = append(result.Groups, name)
		}
		index++
		return nil
	}
	finishTree := func() error {
		if stage == "" {
			return nil
		}
		for index < len(names) && prefix+names[index] < tree {
			if err := complete(nil); err != nil {
				return err
			}
		}
		// Directories stay writable until all children have arrived.
		for i := len(members) - 1; i >= 0; i-- {
			if members[i].typeflag == tar.TypeDir {
				if err := root.Chmod(path.Join(stage, members[i].name), members[i].mode); err != nil {
					return err
				}
			}
		}
		if err := ensureRestoreAccountsParent(root, path.Dir(tree)); err != nil {
			return err
		}
		if err := root.RemoveAll(tree); err != nil {
			return err
		}
		if err := root.Rename(path.Join(stage, tree), tree); err != nil {
			return err
		}
		if err := complete(members); err != nil {
			return err
		}
		if err := root.RemoveAll(stage); err != nil {
			return err
		}
		stage, tree, members = "", "", nil
		return nil
	}
	visit := func(entry hostRestoreEntry, content io.Reader) error {
		nextTree := ""
		if strings.HasPrefix(entry.name, prefix) {
			nextTree = strings.Join(strings.Split(entry.name, "/")[:2], "/")
		}
		if tree != "" && nextTree != tree {
			if err := finishTree(); err != nil {
				return err
			}
		}
		if nextTree == "" {
			return nil
		}
		if stage == "" {
			stageName, err := os.MkdirTemp(env.Root, ".opsctl-accounts-restore-")
			if err != nil {
				return err
			}
			stage, tree = filepath.Base(stageName), nextTree
			if err := root.Chmod(stage, 0o700); err != nil {
				return err
			}
		}
		name := path.Join(stage, entry.name)
		if err := mkdirHostRestoreParents(root, path.Dir(name)); err != nil {
			return err
		}
		switch entry.typeflag {
		case tar.TypeDir:
			if err := root.Mkdir(name, 0o700); err != nil {
				return err
			}
			if err := root.Chmod(name, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return err
			}
			_, writeErr := io.Copy(file, content)
			if err := errors.Join(writeErr, file.Close()); err != nil {
				return err
			}
			if err := root.Chmod(name, entry.mode); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := root.Symlink(entry.linkname, name); err != nil {
				return err
			}
		}
		members = append(members, entry)
		return nil
	}
	_, err = streamRestoreAccounts(ctx, env, client, archive.uri, archive.manifestJSON, visit, func() error {
		if err := finishTree(); err != nil {
			return err
		}
		for index < len(names) {
			if err := complete(nil); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// Read a skipped member a block at a time. Homes and folders may be larger
// than memory; validation needs their framing, never an accumulated copy.
func discardRestoreMember(reader io.Reader) error {
	var buffer [32768]byte
	for {
		_, err := reader.Read(buffer[:])
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
