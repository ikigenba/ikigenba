package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/accounts"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/hardening"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

type restoreAccountsArchive struct {
	manifest accounts.Manifest
	entries  []hostRestoreEntry
}

func loadRestoreAccounts(ctx context.Context, env host.Env, client cloud.Client, objects []cloud.Object, selectedURI, prefix string, result *HostRestoreResult) (*restoreAccountsArchive, error) {
	uri := strings.TrimSuffix(selectedURI, ".tar.zst") + ".accounts.tar.zst"
	found := slices.ContainsFunc(objects, func(object cloud.Object) bool { return object.URI == uri })
	if !found {
		result.AccountsReady = true
		return nil, nil
	}
	result.AccountsObject = "host/" + strings.TrimPrefix(uri, prefix)
	compressed, err := readRestoreAccountsObject(ctx, client, uri)
	result.AccountsSize = int64(len(compressed))
	if err != nil {
		return nil, err
	}
	archive, err := decompressHostArchive(ctx, env.Execute, compressed)
	if err != nil {
		return nil, err
	}
	decoded, err := validateRestoreAccountsArchive(archive)
	if err != nil {
		return nil, fmt.Errorf("validate %q: %w", uri, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := guardRestoreAccountFiles(env.Root, len(decoded.manifest.Users) != 0); err != nil {
		return nil, err
	}
	if err := accounts.CheckManifest(ctx, env, decoded.manifest); err != nil {
		return nil, err
	}
	result.AccountsReady = true
	return &decoded, nil
}

func guardRestoreAccountFiles(rootName string, shadow bool) error {
	root, err := os.OpenRoot(rootName)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	names := []string{"etc", "etc/passwd", "etc/group"}
	if shadow {
		names = append(names, "etc/shadow")
	}
	for _, name := range names {
		info, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("account path /%s is a symbolic link", name)
		}
	}
	return nil
}

func prepareRestoreAccountsParent(rootName, parent string) error {
	root, err := os.OpenRoot(rootName)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return ensureRestoreAccountsParent(root, parent)
}

func readRestoreAccountsObject(ctx context.Context, client cloud.Client, uri string) ([]byte, error) {
	reader, err := client.GetObject(ctx, uri)
	if err != nil {
		if reader != nil {
			err = errors.Join(err, reader.Close())
		}
		return nil, fmt.Errorf("download %q: %w", uri, err)
	}
	if reader == nil {
		return nil, fmt.Errorf("download %q: object reader is not configured", uri)
	}
	data, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if err := errors.Join(readErr, closeErr, ctx.Err()); err != nil {
		return data, fmt.Errorf("download %q: %w", uri, err)
	}
	return data, nil
}

func validateRestoreAccountsArchive(archive []byte) (restoreAccountsArchive, error) {
	decoded := restoreAccountsArchive{}
	if len(archive) < 1024 || len(archive)%512 != 0 || !allZero(archive[len(archive)-1024:]) {
		return decoded, errors.New("invalid accounts tar archive termination")
	}
	raw := bytes.NewReader(archive)
	reader := tar.NewReader(raw)
	byName := map[string]hostRestoreEntry{}
	manifestFound := false
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			if !allZero(archive[len(archive)-raw.Len():]) {
				return decoded, errors.New("invalid data after accounts tar archive end")
			}
			break
		}
		if err != nil {
			return decoded, fmt.Errorf("read accounts tar archive: %w", err)
		}
		name := strings.TrimSuffix(header.Name, "/")
		if name == "" || path.IsAbs(name) || path.Clean(name) != name || strings.Contains(name, "//") {
			return decoded, fmt.Errorf("unsafe accounts archive entry %q", header.Name)
		}
		for component := range strings.SplitSeq(name, "/") {
			if component == "" || component == "." || component == ".." {
				return decoded, fmt.Errorf("unsafe accounts archive entry %q", header.Name)
			}
		}
		if _, exists := byName[name]; exists {
			return decoded, fmt.Errorf("duplicate accounts archive entry %q", header.Name)
		}
		entry := hostRestoreEntry{name: name, typeflag: header.Typeflag, mode: restoredFileMode(header.Mode), linkname: header.Linkname, uid: header.Uid, gid: header.Gid, access: header.PAXRecords["SCHILY.acl.access"], defaults: header.PAXRecords["SCHILY.acl.default"]}
		for _, key := range []string{"SCHILY.acl.access", "SCHILY.acl.default"} {
			if value, exists := header.PAXRecords[key]; exists && !validRestoreACL(value) {
				return decoded, fmt.Errorf("invalid %s in %q", key, name)
			}
		}
		switch header.Typeflag {
		case tar.TypeReg, byte(0):
			entry.typeflag = tar.TypeReg
			entry.data, err = io.ReadAll(reader)
			if err != nil {
				return decoded, fmt.Errorf("read accounts archive entry %q: %w", name, err)
			}
		case tar.TypeDir, tar.TypeSymlink:
			if header.Size != 0 {
				return decoded, fmt.Errorf("accounts archive entry %q has unexpected data", name)
			}
		default:
			return decoded, fmt.Errorf("accounts archive entry %q has unsupported type", name)
		}
		if name == "accounts.json" {
			if entry.typeflag != tar.TypeReg {
				return decoded, errors.New("accounts.json is not a regular file")
			}
			if err := decodeRestoreManifest(entry.data, &decoded.manifest); err != nil {
				return decoded, err
			}
			manifestFound = true
		}
		byName[name] = entry
		decoded.entries = append(decoded.entries, entry)
	}
	if !manifestFound {
		return decoded, errors.New("accounts.json is missing")
	}
	for _, entry := range decoded.entries {
		if err := validRestoreAccountsEntry(entry, decoded.manifest, byName); err != nil {
			return decoded, err
		}
	}
	return decoded, nil
}

func decodeRestoreManifest(data []byte, manifest *accounts.Manifest) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return errors.New("accounts.json must hold one JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(manifest); err != nil {
		return fmt.Errorf("decode accounts.json: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("accounts.json must hold one JSON object")
	}
	return nil
}

func validRestoreAccountsEntry(entry hostRestoreEntry, manifest accounts.Manifest, byName map[string]hostRestoreEntry) error {
	if entry.name == "accounts.json" {
		return nil
	}
	parts := strings.Split(entry.name, "/")
	if len(parts) == 3 && parts[0] == "etc" && parts[1] == "ssh" && strings.HasPrefix(parts[2], "ssh_host_") && len(parts[2]) > len("ssh_host_") && entry.typeflag == tar.TypeReg {
		return nil
	}
	if len(parts) == 4 && parts[0] == "etc" && parts[1] == "ssh" && parts[2] == "authorized_keys" && entry.typeflag == tar.TypeReg && slices.ContainsFunc(manifest.Users, func(user accounts.ManifestUser) bool { return user.Name == parts[3] }) {
		return nil
	}
	tree := false
	if len(parts) >= 2 {
		switch parts[0] {
		case "home":
			tree = slices.ContainsFunc(manifest.Users, func(user accounts.ManifestUser) bool { return user.Name == parts[1] })
		case "srv":
			tree = slices.ContainsFunc(manifest.Groups, func(group accounts.ManifestGroup) bool { return group.Slug == parts[1] })
		}
	}
	if !tree || len(parts) == 2 && entry.typeflag != tar.TypeDir {
		return fmt.Errorf("accounts archive entry %q is outside manifest trees", entry.name)
	}
	for depth := 2; depth < len(parts); depth++ {
		parent := strings.Join(parts[:depth], "/")
		ancestor, found := byName[parent]
		if !found || ancestor.typeflag != tar.TypeDir {
			return fmt.Errorf("accounts archive entry %q has no directory parent %q", entry.name, parent)
		}
	}
	return nil
}

func validRestoreACL(value string) bool {
	for entry := range strings.SplitSeq(value, ",") {
		parts := strings.Split(entry, ":")
		if len(parts) != 3 {
			return false
		}
		if parts[0] != "user" && parts[0] != "group" && parts[0] != "mask" && parts[0] != "other" {
			return false
		}
		for _, c := range parts[1] {
			if c < '0' || c > '9' {
				return false
			}
		}
		p := parts[2]
		if len(p) != 3 || p[0] != 'r' && p[0] != '-' || p[1] != 'w' && p[1] != '-' || p[2] != 'x' && p[2] != '-' {
			return false
		}
	}
	return true
}

func replayRestoreAccounts(ctx context.Context, env host.Env, archive restoreAccountsArchive, result *HostRestoreResult) error {
	fail := func(step string, err error) error { result.FailedStep = step; return err }
	if err := accounts.EnsureShellGroup(ctx, env); err != nil {
		return fail("groups", err)
	}
	groups := slices.Clone(archive.manifest.Groups)
	slices.SortFunc(groups, func(a, b accounts.ManifestGroup) int { return strings.Compare(a.Slug, b.Slug) })
	for _, group := range groups {
		if err := prepareRestoreAccountsParent(env.Root, "srv"); err != nil {
			return fail("groups", err)
		}
		members := restoreAccountMembers(archive.entries, "srv/"+group.Slug)
		if err := replaceRestoreAccountsMembers(ctx, env, members); err != nil {
			return fail("groups", err)
		}
		if err := restoreAccountsMetadata(ctx, env, members); err != nil {
			return fail("groups", err)
		}
		if err := accounts.RestoreGroup(ctx, env, group); err != nil {
			return fail("groups", err)
		}
		result.Groups = append(result.Groups, group.Slug)
	}
	result.GroupsRestored = true
	users := slices.Clone(archive.manifest.Users)
	slices.SortFunc(users, func(a, b accounts.ManifestUser) int { return strings.Compare(a.Name, b.Name) })
	for _, user := range users {
		home := "home/" + user.Name
		key := strings.TrimPrefix(hardening.KeysDir, "/") + "/" + user.Name
		members := make([]hostRestoreEntry, 0)
		for _, entry := range archive.entries {
			if entry.name == home || strings.HasPrefix(entry.name, home+"/") || entry.name == key {
				members = append(members, entry)
			}
		}
		if err := replaceRestoreAccountsMembers(ctx, env, members); err != nil {
			return fail("users", err)
		}
		if err := restoreAccountsMetadata(ctx, env, members); err != nil {
			return fail("users", err)
		}
		if err := accounts.RestoreUser(ctx, env, user); err != nil {
			return fail("users", err)
		}
		result.Users = append(result.Users, user.Name)
		if user.State == accounts.StateLocked {
			result.Locked = append(result.Locked, user.Name)
		}
	}
	_, err := os.Stat(filepath.Join(env.Root, release.UserUnitDir, release.TurnsTimer))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail("users", err)
	}
	result.TimersInstalled = err == nil
	result.UsersRestored = true
	count, err := restoreAccountsSSH(ctx, env, archive.entries)
	if err != nil {
		return fail("ssh", err)
	}
	result.HostKeys = count
	result.SSHRestored = true
	return nil
}

func restoreAccountMembers(entries []hostRestoreEntry, tree string) []hostRestoreEntry {
	members := make([]hostRestoreEntry, 0)
	for _, entry := range entries {
		if entry.name == tree || strings.HasPrefix(entry.name, tree+"/") {
			members = append(members, entry)
		}
	}
	return members
}

func replaceRestoreAccountsMembers(ctx context.Context, env host.Env, members []hostRestoreEntry) error {
	if len(members) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := os.OpenRoot(env.Root)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	stageName, err := os.MkdirTemp(env.Root, ".opsctl-accounts-restore-")
	if err != nil {
		return err
	}
	stage := filepath.Base(stageName)
	defer func() { _ = root.RemoveAll(stage) }()
	if err := root.Chmod(stage, 0o700); err != nil {
		return err
	}
	if err := populateHostRestoreStage(root, stage, members); err != nil {
		return err
	}
	top := make([]string, 0)
	for _, member := range members {
		name := member.name
		if strings.HasPrefix(name, "home/") || strings.HasPrefix(name, "srv/") {
			name = strings.Join(strings.Split(name, "/")[:2], "/")
		}
		if !slices.Contains(top, name) {
			top = append(top, name)
		}
	}
	for _, name := range top {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := ensureRestoreAccountsParent(root, path.Dir(name)); err != nil {
			return err
		}
		if err := root.RemoveAll(name); err != nil {
			return err
		}
		if err := root.Rename(path.Join(stage, name), name); err != nil {
			return err
		}
	}
	return nil
}

func ensureRestoreAccountsParent(root *os.Root, parent string) error {
	components := strings.Split(parent, "/")
	for index := range components {
		name := strings.Join(components[:index+1], "/")
		info, err := root.Lstat(name)
		if err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("destination parent /%s is not a directory", name)
			}
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if name != "home" && name != "srv" && name != strings.TrimPrefix(hardening.KeysDir, "/") {
			return err
		}
		if err := root.Mkdir(name, 0o755); err != nil {
			return err
		}
		if err := root.Chmod(name, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func restoreAccountsMetadata(ctx context.Context, env host.Env, members []hostRestoreEntry) error {
	var input strings.Builder
	type owner struct{ uid, gid int }
	links := map[owner][]string{}
	for _, entry := range members {
		name := filepath.Join(env.Root, entry.name)
		if entry.typeflag == tar.TypeSymlink {
			pair := owner{entry.uid, entry.gid}
			links[pair] = append(links[pair], name)
			continue
		}
		_, _ = fmt.Fprintf(&input, "# file: %s\n# owner: %d\n# group: %d\n", escapeRestoreACLPath(name), entry.uid, entry.gid)
		if entry.mode&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			flags := []byte("---")
			if entry.mode&os.ModeSetuid != 0 {
				flags[0] = 's'
			}
			if entry.mode&os.ModeSetgid != 0 {
				flags[1] = 's'
			}
			if entry.mode&os.ModeSticky != 0 {
				flags[2] = 't'
			}
			_, _ = fmt.Fprintf(&input, "# flags: %s\n", flags)
		}
		if entry.access != "" {
			input.WriteString(strings.ReplaceAll(entry.access, ",", "\n") + "\n")
		} else {
			for i, label := range []string{"user", "group", "other"} {
				_, _ = fmt.Fprintf(&input, "%s::%s\n", label, restoreACLPermissions(uint32(entry.mode.Perm())>>uint(6-3*i)))
			}
		}
		if entry.defaults != "" {
			for acl := range strings.SplitSeq(entry.defaults, ",") {
				input.WriteString("default:" + acl + "\n")
			}
		}
		input.WriteByte('\n')
	}
	if input.Len() != 0 {
		if err := restoreAccountsCommand(ctx, env, "setfacl", host.Command{Name: "setfacl", Args: []string{"--restore=-"}, Stdin: strings.NewReader(input.String())}); err != nil {
			return err
		}
	}
	pairs := make([]owner, 0, len(links))
	for pair := range links {
		pairs = append(pairs, pair)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].uid != pairs[j].uid {
			return pairs[i].uid < pairs[j].uid
		}
		return pairs[i].gid < pairs[j].gid
	})
	for _, pair := range pairs {
		args := []string{"--no-dereference", strconv.Itoa(pair.uid) + ":" + strconv.Itoa(pair.gid), "--"}
		args = append(args, links[pair]...)
		if err := restoreAccountsCommand(ctx, env, "chown", host.Command{Name: "chown", Args: args}); err != nil {
			return err
		}
	}
	return nil
}

func escapeRestoreACLPath(value string) string {
	var escaped strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' || value[i] == '\n' || value[i] == '\r' {
			_, _ = fmt.Fprintf(&escaped, "\\%03o", value[i])
		} else {
			escaped.WriteByte(value[i])
		}
	}
	return escaped.String()
}

func restoreACLPermissions(bits uint32) string {
	p := []byte("---")
	if bits&4 != 0 {
		p[0] = 'r'
	}
	if bits&2 != 0 {
		p[1] = 'w'
	}
	if bits&1 != 0 {
		p[2] = 'x'
	}
	return string(p)
}

func restoreAccountsCommand(ctx context.Context, env host.Env, label string, command host.Command) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	result, err := env.Execute(ctx, command)
	if err != nil || result.ExitCode != 0 {
		return &host.CommandError{Label: label, Result: result, Err: err}
	}
	return nil
}

func restoreAccountsSSH(ctx context.Context, env host.Env, entries []hostRestoreEntry) (int, error) {
	members := make([]hostRestoreEntry, 0)
	count := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.name, "etc/ssh/ssh_host_") {
			members = append(members, entry)
			if !strings.HasSuffix(entry.name, ".pub") {
				count++
			}
		}
	}
	root, err := os.OpenRoot(env.Root)
	if err != nil {
		return 0, err
	}
	defer func() { _ = root.Close() }()
	var stage string
	if len(members) != 0 {
		name, err := os.MkdirTemp(env.Root, ".opsctl-ssh-restore-")
		if err != nil {
			return 0, err
		}
		stage = filepath.Base(name)
		defer func() { _ = root.RemoveAll(stage) }()
		if err := root.Chmod(stage, 0o700); err != nil {
			return 0, err
		}
		if err := populateHostRestoreStage(root, stage, members); err != nil {
			return 0, err
		}
	}
	type previousKey struct {
		name, saved string
		existed     bool
	}
	previous := make([]previousKey, 0, len(members))
	rollback := func(cause error) (int, error) {
		for i := len(previous) - 1; i >= 0; i-- {
			key := previous[i]
			if err := root.RemoveAll(key.name); err != nil {
				cause = errors.Join(cause, err)
				continue
			}
			if key.existed {
				if err := root.Rename(key.saved, key.name); err != nil {
					cause = errors.Join(cause, err)
				}
			}
		}
		return 0, cause
	}
	if len(members) != 0 {
		if err := ensureRestoreAccountsParent(root, "etc/ssh"); err != nil {
			return 0, err
		}
	}
	for index, member := range members {
		if err := ctx.Err(); err != nil {
			return rollback(err)
		}
		saved := path.Join(stage, "previous-"+strconv.Itoa(index))
		_, err := root.Lstat(member.name)
		existed := err == nil
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return rollback(err)
		}
		if existed {
			if err := root.Rename(member.name, saved); err != nil {
				return rollback(err)
			}
		}
		previous = append(previous, previousKey{name: member.name, saved: saved, existed: existed})
		if err := root.Rename(path.Join(stage, member.name), member.name); err != nil {
			return rollback(err)
		}
	}
	if err := restoreAccountsMetadata(ctx, env, members); err != nil {
		return rollback(err)
	}
	if err := restoreAccountsCommand(ctx, env, "sshd -t", host.Command{Name: "sshd", Args: []string{"-t", "-f", filepath.Join(env.Root, "etc/ssh/sshd_config")}}); err != nil {
		return rollback(err)
	}
	if err := restoreAccountsCommand(ctx, env, "systemctl reload", host.Command{Name: "systemctl", Args: []string{"reload", "sshd.service"}}); err != nil {
		return rollback(err)
	}
	return count, nil
}
