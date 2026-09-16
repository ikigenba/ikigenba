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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// FileResult describes one service file-backup attempt.
type FileResult struct {
	Service string
	Object  string
	Size    int64
	Err     error
}

type fileBackupPlan struct {
	prefix   string
	basename string
	services []apps.Service
	client   cloud.Client
}

// Files archives and uploads the ordinary files for every selected service.
func Files(ctx context.Context, env host.Env, cloudEnv cloud.Env, store config.Store, service string) ([]FileResult, error) {
	prefix, region, err := fileBackupConfiguration(store, service)
	if err != nil {
		return nil, err
	}
	plan, err := prepareFileBackup(ctx, env, cloudEnv, prefix, region, service)
	if err != nil {
		return nil, err
	}
	return executeFileBackup(ctx, env, plan)
}

func fileBackupConfiguration(store config.Store, service string) (string, string, error) {
	prefix, err := requiredSetting(store, "backup.s3_uri")
	if err != nil {
		return "", "", err
	}
	if err := validateS3Prefix(prefix); err != nil {
		return "", "", fmt.Errorf("backup.s3_uri: %w", err)
	}
	region, err := requiredSetting(store, "aws.region")
	if err != nil {
		return "", "", err
	}
	if service != "" && invalidFileServiceName(service) {
		return "", "", fmt.Errorf("invalid service %q", service)
	}
	return prefix, region, nil
}

func prepareFileBackup(ctx context.Context, env host.Env, cloudEnv cloud.Env, prefix, region, service string) (fileBackupPlan, error) {
	if err := ctx.Err(); err != nil {
		return fileBackupPlan{}, fmt.Errorf("backup files: %w", err)
	}

	services, err := selectFileServices(env.Root, service)
	if err != nil {
		return fileBackupPlan{}, err
	}
	if env.Now == nil {
		return fileBackupPlan{}, errors.New("backup files: host time is not configured")
	}
	plan := fileBackupPlan{
		prefix:   prefix,
		basename: env.Now().UTC().Format(time.RFC3339Nano) + ".tar.zst",
		services: services,
	}
	if len(services) == 0 {
		return plan, nil
	}
	if cloudEnv.Open == nil {
		return fileBackupPlan{}, errors.New("backup files: cloud access is not configured")
	}
	plan.client, err = cloudEnv.Open(ctx, region)
	if err != nil {
		return fileBackupPlan{}, fmt.Errorf("open backup storage: %w", err)
	}
	return plan, nil
}

func executeFileBackup(ctx context.Context, env host.Env, plan fileBackupPlan) ([]FileResult, error) {
	results := make([]FileResult, 0, len(plan.services))
	for _, selected := range plan.services {
		if err := ctx.Err(); err != nil {
			return results, fmt.Errorf("backup files: %w", err)
		}
		result := FileResult{Service: selected.Name}
		if invalidFileServiceName(selected.Name) {
			result.Err = fmt.Errorf("invalid service %q", selected.Name)
			results = append(results, result)
			continue
		}
		if selected.ManifestError != nil {
			result.Err = fmt.Errorf("service %q: %w", selected.Name, selected.ManifestError)
			results = append(results, result)
			continue
		}

		compressed, archiveErr := serviceArchive(ctx, env, selected)
		if archiveErr != nil {
			result.Err = archiveErr
			results = append(results, result)
			if interrupted := interruption(ctx, archiveErr); interrupted != nil {
				return results, fmt.Errorf("backup files: %w", interrupted)
			}
			continue
		}

		object := strings.TrimSuffix(plan.prefix, "/") + "/" + selected.Name + "/" + plan.basename
		uploadErr := plan.client.PutObject(ctx, object, bytes.NewReader(compressed))
		if uploadErr != nil {
			result.Err = fmt.Errorf("upload %q: %w", object, uploadErr)
			results = append(results, result)
			if interrupted := interruption(ctx, uploadErr); interrupted != nil {
				return results, fmt.Errorf("backup files: %w", interrupted)
			}
			continue
		}
		result.Object = plan.basename
		result.Size = int64(len(compressed))
		results = append(results, result)
	}
	return results, nil
}

func interruption(ctx context.Context, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func selectFileServices(root, service string) ([]apps.Service, error) {
	if service == "" {
		services, err := apps.Discover(root)
		if err != nil {
			return nil, fmt.Errorf("discover backup services: %w", err)
		}
		return services, nil
	}
	selected, found, err := discoverFileService(root, service)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("no service " + singleQuotedDiagnostic(service))
	}
	return []apps.Service{selected}, nil
}

func discoverFileService(root, name string) (apps.Service, bool, error) {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return apps.Service{}, false, fmt.Errorf("discover service %q: open root: %w", name, err)
	}
	defer func() { _ = filesystem.Close() }()

	servicePath := path.Join("opt", name)
	info, err := filesystem.Lstat(servicePath)
	if errors.Is(err, os.ErrNotExist) || err == nil && !info.IsDir() {
		return apps.Service{}, false, nil
	}
	if err != nil {
		return apps.Service{}, false, fmt.Errorf("discover service %q: %w", name, err)
	}
	etc, err := rootedDirectory(filesystem, path.Join(servicePath, "etc"))
	if err != nil {
		return apps.Service{}, false, fmt.Errorf("discover service %q: %w", name, err)
	}
	state, err := rootedDirectory(filesystem, path.Join(servicePath, "state"))
	if err != nil {
		return apps.Service{}, false, fmt.Errorf("discover service %q: %w", name, err)
	}
	if !etc && !state {
		return apps.Service{}, false, nil
	}

	selected := apps.Service{Name: name}
	data, readErr := filesystem.ReadFile(path.Join(servicePath, "etc", "manifest.toml"))
	switch {
	case errors.Is(readErr, os.ErrNotExist):
	case readErr != nil:
		selected.ManifestError = fmt.Errorf("read manifest for %q: %w", name, readErr)
	default:
		manifest, parseErr := apps.ParseManifest(data)
		switch {
		case parseErr != nil:
			selected.ManifestError = parseErr
		case manifest.App != "" && manifest.App != name:
			selected.ManifestError = fmt.Errorf("manifest app %q does not match service directory %q", manifest.App, name)
		default:
			selected.Manifest = &manifest
		}
	}
	return selected, true, nil
}

func rootedDirectory(filesystem *os.Root, name string) (bool, error) {
	info, err := filesystem.Stat(name)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

func invalidFileServiceName(name string) bool {
	return name == "" || name == "." || name == ".." || name == "host" || name == "deploy" || strings.ContainsAny(name, "/\x00")
}

func singleQuotedDiagnostic(value string) string {
	quoted := strconv.Quote(value)
	return "'" + strings.ReplaceAll(quoted[1:len(quoted)-1], "'", "\\'") + "'"
}

func serviceArchive(ctx context.Context, env host.Env, service apps.Service) ([]byte, error) {
	filesystem, err := os.OpenRoot(env.Root)
	if err != nil {
		return nil, fmt.Errorf("archive %q: open root: %w", service.Name, err)
	}
	defer func() { _ = filesystem.Close() }()

	resolver := identityResolver{ctx: ctx, execute: env.Execute, users: make(map[uint32]string), groups: make(map[uint32]string)}
	var archive bytes.Buffer
	tarWriter := tar.NewWriter(&archive)
	for _, tree := range []string{"etc", "state"} {
		if err := archiveTree(ctx, filesystem, tarWriter, &resolver, service, tree); err != nil {
			_ = tarWriter.Close()
			return nil, fmt.Errorf("archive %q: %w", service.Name, err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		return nil, fmt.Errorf("archive %q: %w", service.Name, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("archive %q: %w", service.Name, err)
	}
	return compressArchive(ctx, env.Execute, service.Name, archive.Bytes())
}

func archiveTree(ctx context.Context, filesystem *os.Root, writer *tar.Writer, resolver *identityResolver, service apps.Service, tree string) error {
	servicePath := path.Join("opt", service.Name)
	treePath := path.Join(servicePath, tree)
	if _, err := filesystem.Lstat(treePath); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}

	exclusions := databaseExclusions(service.Manifest)
	return fs.WalkDir(filesystem.FS(), treePath, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		member := strings.TrimPrefix(name, servicePath+"/")
		if archiveExcluded(member, exclusions) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := filesystem.Lstat(name)
		if err != nil {
			return err
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = filesystem.Readlink(name)
			if err != nil {
				return err
			}
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		header.Name = member
		if info.IsDir() {
			header.Name += "/"
		}
		stat := info.Sys().(*syscall.Stat_t)
		header.Uid = int(stat.Uid)
		header.Gid = int(stat.Gid)
		header.Uname, err = resolver.user(stat.Uid)
		if err != nil {
			return err
		}
		header.Gname, err = resolver.group(stat.Gid)
		if err != nil {
			return err
		}
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		file, err := filesystem.Open(name)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		return errors.Join(copyErr, closeErr)
	})
}

func databaseExclusions(manifest *apps.Manifest) []string {
	if manifest == nil || manifest.Database == nil {
		return nil
	}
	return DatabaseFiles(*manifest.Database)
}

func archiveExcluded(member string, exclusions []string) bool {
	for _, excluded := range exclusions {
		if member == excluded || strings.HasPrefix(member, excluded+"/") {
			return true
		}
	}
	return false
}

type identityResolver struct {
	ctx     context.Context
	execute func(context.Context, host.Command) (host.Result, error)
	users   map[uint32]string
	groups  map[uint32]string
}

func (resolver *identityResolver) user(uid uint32) (string, error) {
	if name, exists := resolver.users[uid]; exists {
		return name, nil
	}
	name, err := resolver.lookup("passwd", uid)
	if err == nil {
		resolver.users[uid] = name
	}
	return name, err
}

func (resolver *identityResolver) group(gid uint32) (string, error) {
	if name, exists := resolver.groups[gid]; exists {
		return name, nil
	}
	name, err := resolver.lookup("group", gid)
	if err == nil {
		resolver.groups[gid] = name
	}
	return name, err
}

func (resolver *identityResolver) lookup(database string, id uint32) (string, error) {
	if resolver.execute == nil {
		return "", errors.New("identity lookup: host execution is not configured")
	}
	value := strconv.FormatUint(uint64(id), 10)
	result, err := resolver.execute(resolver.ctx, host.Command{Name: "getent", Args: []string{database, value}})
	label := "lookup " + database + " identity " + value
	if err != nil {
		var commandErr *host.CommandError
		if errors.As(err, &commandErr) {
			return "", err
		}
		return "", &host.CommandError{Label: label, Result: result, Err: err}
	}
	if result.ExitCode == 2 {
		return "", nil
	}
	if result.ExitCode != 0 {
		return "", &host.CommandError{Label: label, Result: result}
	}
	fields := strings.Split(strings.TrimSpace(string(result.Stdout)), ":")
	if len(fields) < 3 || fields[0] == "" || fields[2] != value {
		return "", fmt.Errorf("%s: invalid response", label)
	}
	return fields[0], nil
}

func compressArchive(ctx context.Context, execute func(context.Context, host.Command) (host.Result, error), service string, archive []byte) ([]byte, error) {
	if execute == nil {
		return nil, errors.New("compress archive: host execution is not configured")
	}
	result, err := execute(ctx, host.Command{
		Name:  "zstd",
		Args:  []string{"--quiet", "--stdout"},
		Stdin: bytes.NewReader(archive),
	})
	label := "compress " + strconv.Quote(service) + " archive"
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
	if len(result.Stdout) < 4 || !bytes.Equal(result.Stdout[:4], []byte{0x28, 0xb5, 0x2f, 0xfd}) {
		return nil, fmt.Errorf("%s: invalid zstd output", label)
	}
	return result.Stdout, nil
}
