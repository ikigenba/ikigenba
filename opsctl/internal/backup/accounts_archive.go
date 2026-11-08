package backup

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/accounts"
	"github.com/ikigenba/ikigenba/opsctl/internal/hardening"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

type accountArchiveEntry struct {
	name string
	info fs.FileInfo
}

type accountArchiveMetadata struct {
	uid, gid         int
	access, defaults []string
	owner, group     bool
}

func accountsArchive(ctx context.Context, env host.Env, output io.Writer) error {
	filesystem, err := os.OpenRoot(env.Root)
	if err != nil {
		return accountsArchiveReadError(env.Root, err)
	}
	defer func() { _ = filesystem.Close() }()
	if err := hostArchiveParents(filesystem, "etc/passwd"); err != nil {
		return err
	}
	for _, name := range []string{"etc/passwd", "etc/group", "etc/shadow"} {
		info, err := filesystem.Lstat(name)
		if err != nil {
			return accountsArchiveReadError(env.Root, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("/%s: account source is a symbolic link", name)
		}
	}
	manifest, err := accounts.ReadManifest(ctx, env)
	if err != nil {
		return err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	paths, entries, err := collectAccountArchive(ctx, filesystem, manifest)
	if err != nil {
		return accountsArchiveReadError(env.Root, err)
	}
	metadata, links, err := readAccountArchiveMetadata(ctx, env, paths, entries)
	if err != nil {
		return err
	}
	writer := tar.NewWriter(output)
	if err := writer.WriteHeader(&tar.Header{Name: "accounts.json", Typeflag: tar.TypeReg, Mode: 0o600, Size: int64(len(data))}); err != nil {
		return err
	}
	if _, err := writer.Write(data); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := writeAccountArchiveEntry(filesystem, writer, env.Root, entry, metadata, links); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return nil
}

func collectAccountArchive(ctx context.Context, root *os.Root, manifest accounts.Manifest) ([]string, []accountArchiveEntry, error) {
	var paths []string
	var entries []accountArchiveEntry
	add := func(name string, tree bool) error {
		if err := hostArchiveParents(root, name); err != nil {
			return err
		}
		info, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if tree && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) || !tree && !info.Mode().IsRegular() {
			return nil
		}
		paths = append(paths, name)
		if !tree {
			entries = append(entries, accountArchiveEntry{name, info})
			return nil
		}
		return fs.WalkDir(root.FS(), name, func(member string, _ fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			entryInfo, err := root.Lstat(member)
			if err != nil {
				return err
			}
			if entryInfo.IsDir() || entryInfo.Mode().IsRegular() || entryInfo.Mode()&os.ModeSymlink != 0 {
				entries = append(entries, accountArchiveEntry{member, entryInfo})
			}
			return nil
		})
	}
	if err := hostArchiveParents(root, "etc/ssh"); err != nil {
		return nil, nil, err
	}
	sshInfo, err := root.Lstat("etc/ssh")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	if err == nil && sshInfo.Mode()&os.ModeSymlink != 0 {
		return nil, nil, errors.New("/etc/ssh: source directory is a symbolic link")
	}
	ssh, err := root.Open("etc/ssh")
	if err == nil {
		names, readErr := ssh.Readdirnames(-1)
		closeErr := ssh.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return nil, nil, err
		}
		slices.Sort(names)
		for _, name := range names {
			if strings.HasPrefix(name, "ssh_host_") {
				if err := add("etc/ssh/"+name, false); err != nil {
					return nil, nil, err
				}
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	for _, user := range manifest.Users {
		if err := add(strings.TrimPrefix(hardening.KeysDir, "/")+"/"+user.Name, false); err != nil {
			return nil, nil, err
		}
	}
	for _, user := range manifest.Users {
		if err := add("home/"+user.Name, true); err != nil {
			return nil, nil, err
		}
	}
	for _, group := range manifest.Groups {
		if err := add("srv/"+group.Slug, true); err != nil {
			return nil, nil, err
		}
	}
	return paths, entries, nil
}

func readAccountArchiveMetadata(ctx context.Context, env host.Env, paths []string, entries []accountArchiveEntry) (map[string]accountArchiveMetadata, map[string]accountArchiveMetadata, error) {
	metadata := make(map[string]accountArchiveMetadata)
	links := make(map[string]accountArchiveMetadata)
	if len(paths) == 0 {
		return metadata, links, nil
	}
	resolved := make([]string, len(paths))
	for i, name := range paths {
		resolved[i] = filepath.Join(env.Root, filepath.FromSlash(name))
	}
	output, err := accountArchiveCommand(ctx, env, "getfacl", append([]string{"--absolute-names", "--numeric", "--no-effective", "--physical", "--recursive", "--"}, resolved...))
	if err != nil {
		return nil, nil, err
	}
	metadata, err = parseAccountArchiveACLs(output)
	if err != nil {
		return nil, nil, err
	}
	hasLinks := false
	for _, entry := range entries {
		if entry.info.Mode()&os.ModeSymlink != 0 {
			hasLinks = true
		}
	}
	if hasLinks {
		args := append([]string{"-P"}, resolved...)
		args = append(args, "-type", "l", "-printf", "%U %G %p\\0")
		output, err = accountArchiveCommand(ctx, env, "find", args)
		if err != nil {
			return nil, nil, err
		}
		links, err = parseAccountArchiveLinks(output)
		if err != nil {
			return nil, nil, err
		}
	}
	for _, entry := range entries {
		name := filepath.Join(env.Root, filepath.FromSlash(entry.name))
		values, ok := metadata[name]
		if entry.info.Mode()&os.ModeSymlink != 0 {
			values, ok = links[name]
		}
		if !ok || !values.owner || !values.group {
			return nil, nil, fmt.Errorf("metadata missing for /%s", entry.name)
		}
	}
	return metadata, links, nil
}

func accountArchiveCommand(ctx context.Context, env host.Env, name string, args []string) ([]byte, error) {
	result, err := env.Execute(ctx, host.Command{Name: name, Args: args})
	if err != nil || result.ExitCode != 0 {
		return nil, &host.CommandError{Label: name, Result: result, Err: err}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result.Stdout, nil
}

func writeAccountArchiveEntry(root *os.Root, writer *tar.Writer, rootName string, entry accountArchiveEntry, metadata, links map[string]accountArchiveMetadata) error {
	link := ""
	if entry.info.Mode()&os.ModeSymlink != 0 {
		var err error
		link, err = root.Readlink(entry.name)
		if err != nil {
			return accountsArchiveReadError(rootName, err)
		}
	}
	header, err := tar.FileInfoHeader(entry.info, link)
	if err != nil {
		return err
	}
	header.Name = path.Clean(entry.name)
	if entry.info.IsDir() {
		header.Name += "/"
	}
	values := metadata[filepath.Join(rootName, filepath.FromSlash(entry.name))]
	if entry.info.Mode()&os.ModeSymlink != 0 {
		values = links[filepath.Join(rootName, filepath.FromSlash(entry.name))]
	}
	header.Uid, header.Gid = values.uid, values.gid
	header.Uname, header.Gname = "", ""
	header.PAXRecords = make(map[string]string)
	extended := false
	for _, acl := range values.access {
		if !strings.HasPrefix(acl, "user::") && !strings.HasPrefix(acl, "group::") && !strings.HasPrefix(acl, "other::") {
			extended = true
		}
	}
	if extended {
		header.PAXRecords["SCHILY.acl.access"] = strings.Join(values.access, ",")
	}
	if len(values.defaults) > 0 {
		header.PAXRecords["SCHILY.acl.default"] = strings.Join(values.defaults, ",")
	}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	if !entry.info.Mode().IsRegular() {
		return nil
	}
	file, err := root.Open(entry.name)
	if err != nil {
		return accountsArchiveReadError(rootName, err)
	}
	_, copyErr := io.Copy(writer, file)
	closeErr := file.Close()
	return accountsArchiveReadError(rootName, errors.Join(copyErr, closeErr))
}

func accountsArchiveReadError(root string, err error) error {
	if err == nil {
		return nil
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		name := pathErr.Path
		if filepath.IsAbs(name) {
			name = strings.TrimPrefix(name, root)
		} else {
			name = "/" + name
		}
		return fmt.Errorf("%s: %w", name, pathErr.Err)
	}
	return err
}

func parseAccountArchiveACLs(output []byte) (map[string]accountArchiveMetadata, error) {
	result := make(map[string]accountArchiveMetadata)
	name := ""
	flush := func() error {
		if name != "" {
			v := result[name]
			if !v.owner || !v.group || len(v.access) == 0 {
				return fmt.Errorf("invalid getfacl metadata for %q", name)
			}
		}
		return nil
	}
	for _, line := range strings.Split(string(output), "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# file: ") {
			if err := flush(); err != nil {
				return nil, err
			}
			var err error
			name, err = unescapeAccountACLPath(strings.TrimPrefix(line, "# file: "))
			if err != nil || name == "" {
				return nil, errors.New("invalid getfacl file name")
			}
			if _, ok := result[name]; ok {
				return nil, fmt.Errorf("duplicate getfacl metadata for %q", name)
			}
			result[name] = accountArchiveMetadata{}
			continue
		}
		if name == "" {
			return nil, errors.New("getfacl output has no file section")
		}
		value := result[name]
		switch {
		case strings.HasPrefix(line, "# owner: "):
			n, err := accountMetadataID(strings.TrimPrefix(line, "# owner: "))
			if err != nil || value.owner {
				return nil, errors.New("invalid getfacl owner")
			}
			value.uid, value.owner = n, true
		case strings.HasPrefix(line, "# group: "):
			n, err := accountMetadataID(strings.TrimPrefix(line, "# group: "))
			if err != nil || value.group {
				return nil, errors.New("invalid getfacl group")
			}
			value.gid, value.group = n, true
		case strings.HasPrefix(line, "# flags: "):
		default:
			acl := line
			isDefault := strings.HasPrefix(acl, "default:")
			if isDefault {
				acl = strings.TrimPrefix(acl, "default:")
			}
			if !validAccountACLEntry(acl) {
				return nil, fmt.Errorf("invalid getfacl ACL %q", line)
			}
			if isDefault {
				value.defaults = append(value.defaults, acl)
			} else {
				value.access = append(value.access, acl)
			}
		}
		result[name] = value
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return result, nil
}

func accountMetadataID(value string) (int, error) {
	if value == "" {
		return 0, errors.New("empty metadata id")
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, errors.New("invalid metadata id")
		}
	}
	return strconv.Atoi(value)
}

func validAccountACLEntry(value string) bool {
	parts := strings.Split(value, ":")
	if len(parts) != 3 || len(parts[2]) != 3 {
		return false
	}
	if parts[0] != "user" && parts[0] != "group" && parts[0] != "mask" && parts[0] != "other" {
		return false
	}
	if parts[1] != "" {
		if parts[0] != "user" && parts[0] != "group" {
			return false
		}
		if _, err := accountMetadataID(parts[1]); err != nil {
			return false
		}
	}
	for i, c := range parts[2] {
		if c != '-' && c != rune("rwx"[i]) {
			return false
		}
	}
	return true
}

func unescapeAccountACLPath(value string) (string, error) {
	var result strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '\\' {
			result.WriteByte(value[i])
			continue
		}
		if i+3 >= len(value) {
			return "", errors.New("short getfacl escape")
		}
		n, err := strconv.ParseUint(value[i+1:i+4], 8, 8)
		if err != nil {
			return "", err
		}
		result.WriteByte(byte(n))
		i += 3
	}
	return result.String(), nil
}

func parseAccountArchiveLinks(output []byte) (map[string]accountArchiveMetadata, error) {
	result := make(map[string]accountArchiveMetadata)
	if len(output) > 0 && output[len(output)-1] != 0 {
		return nil, errors.New("unterminated find metadata")
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(output), "\x00"), "\x00") {
		if line == "" {
			continue
		}
		uid, rest, ok := strings.Cut(line, " ")
		if !ok {
			return nil, errors.New("invalid find metadata")
		}
		gid, name, ok := strings.Cut(rest, " ")
		if !ok || name == "" {
			return nil, errors.New("invalid find metadata")
		}
		u, err := accountMetadataID(uid)
		if err != nil {
			return nil, err
		}
		g, err := accountMetadataID(gid)
		if err != nil {
			return nil, err
		}
		if _, ok := result[name]; ok {
			return nil, errors.New("duplicate find metadata")
		}
		result[name] = accountArchiveMetadata{uid: u, gid: g, owner: true, group: true}
	}
	return result, nil
}

// hostArchiveParents refuses traversal through a source ancestor link. A link
// which is itself an archive member is still preserved without following it.
func hostArchiveParents(root *os.Root, name string) error {
	parent := path.Dir(name)
	if parent == "." {
		return nil
	}
	current := ""
	for _, part := range strings.Split(parent, "/") {
		current = path.Join(current, part)
		info, err := root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("/%s: source ancestor is a symbolic link", current)
		}
		if !info.IsDir() {
			return fmt.Errorf("/%s: source ancestor is not a directory", current)
		}
	}
	return nil
}
