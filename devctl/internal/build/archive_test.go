package build

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestArchivePreparedPublishesExactValidatedPayload(t *testing.T) {
	// R-5LK0-1DC3
	// R-EZIJ-K208
	// R-5LK0-1DC3
	// R-5P7P-6OK6
	// R-F6TX-UOGE
	staged := archiveFixture(t)
	finalPath := filepath.Join(staged.prepared.app.Dir, "dist", "crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz")
	writeTestFile(t, finalPath, []byte("earlier artifact"), 0o600)
	writeTestFile(t, filepath.Join(staged.prepared.app.Dir, "dist", "keep.txt"), []byte("keep"), 0o640)
	var commands []seam.Cmd
	var stdout bytes.Buffer

	err := archivePrepared(context.Background(), staged, &stdout, fakeTarDeps(t, &commands))
	if err != nil {
		t.Fatalf("archivePrepared error = %v", err)
	}
	if stdout.String() != "crm/dist/crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if len(commands) != 1 || commands[0].Path != "tar" {
		t.Fatalf("commands = %#v, want one tar command", commands)
	}

	listing := runTar(t, staged.prepared.app.Dir, "-tJf", finalPath)
	wantMembers := []string{
		"bin/crm",
		"etc/extra.conf",
		"etc/manifest.toml",
		"etc/nested/settings.ini",
		"share/assets/message.txt",
	}
	gotMembers := strings.Split(strings.TrimSuffix(string(listing), "\n"), "\n")
	sort.Strings(gotMembers)
	if !reflect.DeepEqual(gotMembers, wantMembers) {
		t.Fatalf("archive members = %#v, want %#v", gotMembers, wantMembers)
	}
	for _, member := range gotMembers {
		if strings.Contains(member, staged.prepared.sha) {
			t.Fatalf("archive member %q contains version directory", member)
		}
	}

	extracted := t.TempDir()
	runTar(t, staged.prepared.app.Dir, "-xJf", finalPath, "-C", extracted)
	assertTestFile(t, filepath.Join(extracted, "bin", "crm"), []byte("validated executable"), 0o711)
	assertTestFile(t, filepath.Join(extracted, filepath.FromSlash(checkout.ManifestFile)), staged.manifest, 0o644)
	assertTestFile(t, filepath.Join(extracted, "etc", "extra.conf"), []byte("extra\x00bytes"), 0o640)
	assertTestFile(t, filepath.Join(extracted, "etc", "nested", "settings.ini"), []byte("nested\n"), 0o604)
	assertTestFile(t, filepath.Join(extracted, "share", "assets", "message.txt"), []byte("share bytes\n"), 0o444)
	if _, err := os.Lstat(filepath.Join(extracted, "etc", "ignored-link")); !os.IsNotExist(err) {
		t.Fatalf("non-regular etc entry was archived: %v", err)
	}

	assertDistNames(t, staged.prepared.app.Dir,
		".validated-stage", "crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz", "keep.txt")
}

func TestArchivePreparedRejectsStaleManifestBeforeTar(t *testing.T) {
	// R-6UPK-MHDN
	// R-5LK0-1DC3
	// R-5LK0-1DC3
	// R-F6TX-UOGE
	staged := archiveFixture(t)
	writeTestFile(t, filepath.Join(staged.prepared.app.Dir, checkout.ManifestFile), []byte("app = \"crm\"\n# committed\n"), 0o644)
	finalPath := filepath.Join(staged.prepared.app.Dir, "dist", "crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz")
	writeTestFile(t, finalPath, []byte("earlier artifact"), 0o600)
	var stdout bytes.Buffer
	tarCalls := 0

	err := archivePrepared(context.Background(), staged, &stdout, seam.Deps{Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
		tarCalls++
		return seam.Result{}, errors.New("tar must not run")
	}})
	var stale *StaleManifestError
	if !errors.As(err, &stale) || stale.App != "crm" {
		t.Fatalf("error = %#v, want StaleManifestError for crm", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if tarCalls != 0 {
		t.Fatalf("tar calls = %d, want 0", tarCalls)
	}
	assertTestFile(t, finalPath, []byte("earlier artifact"), 0o600)
	assertDistNames(t, staged.prepared.app.Dir, ".validated-stage", "crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz")
}

func TestArchivePreparedTarFailurePreservesPriorArtifact(t *testing.T) {
	// R-5LK0-1DC3
	// R-EZIJ-K208
	// R-5LK0-1DC3
	// R-F6TX-UOGE
	staged := archiveFixture(t)
	finalPath := filepath.Join(staged.prepared.app.Dir, "dist", "crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz")
	writeTestFile(t, finalPath, []byte("earlier artifact"), 0o600)
	var stdout bytes.Buffer

	err := archivePrepared(context.Background(), staged, &stdout, seam.Deps{Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
		if command.Path != "tar" {
			t.Fatalf("command = %#v, want tar", command)
		}
		return seam.Result{ExitCode: 23, Stderr: []byte("xz failed\n")}, nil
	}})
	var processError *ProcessError
	if !errors.As(err, &processError) {
		t.Fatalf("error = %T %v, want *ProcessError", err, err)
	}
	if *processError != (ProcessError{Label: "archive crm", Status: 23, Stderr: "xz failed\n"}) {
		t.Fatalf("ProcessError = %#v", processError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	assertTestFile(t, finalPath, []byte("earlier artifact"), 0o600)
	assertDistNames(t, staged.prepared.app.Dir, ".validated-stage", "crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz")
}

func archiveFixture(t *testing.T) stagedBuild {
	t.Helper()
	appDir := filepath.Join(t.TempDir(), "crm")
	manifest := []byte("app = \"crm\"\n")
	writeTestFile(t, filepath.Join(appDir, checkout.ManifestFile), manifest, 0o600)
	writeTestFile(t, filepath.Join(appDir, "etc", "extra.conf"), []byte("extra\x00bytes"), 0o640)
	writeTestFile(t, filepath.Join(appDir, "etc", "nested", "settings.ini"), []byte("nested\n"), 0o604)
	writeTestFile(t, filepath.Join(appDir, "share", "assets", "message.txt"), []byte("share bytes\n"), 0o444)
	if err := os.Symlink("extra.conf", filepath.Join(appDir, "etc", "ignored-link")); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(appDir, "dist", ".validated-stage", "crm")
	writeTestFile(t, binary, []byte("validated executable"), 0o600)
	return stagedBuild{
		prepared: preparedBuild{app: checkout.App{Name: "crm", Dir: appDir}, sha: prerequisiteHead},
		binary:   binary,
		manifest: manifest,
	}
}

func fakeTarDeps(t *testing.T, commands *[]seam.Cmd) seam.Deps {
	t.Helper()
	return seam.Deps{Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
		if commands != nil {
			*commands = append(*commands, cloneCommand(command))
		}
		if command.Path != "tar" {
			t.Fatalf("archive command = %#v", command)
		}
		archive, directory, members := parseTarCreate(t, command.Args)
		archiveRoot, err := os.OpenRoot(filepath.Dir(archive))
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := archiveRoot.Close(); err != nil {
				t.Error(err)
			}
		}()
		file, err := archiveRoot.Create(filepath.Base(archive))
		if err != nil {
			t.Fatal(err)
		}
		writer := tar.NewWriter(file)
		for _, member := range members {
			path := filepath.Join(directory, filepath.FromSlash(member))
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(directory)
			if err != nil {
				t.Fatal(err)
			}
			contents, err := root.ReadFile(filepath.FromSlash(member))
			closeErr := root.Close()
			if err != nil {
				t.Fatal(err)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
			if err := writer.WriteHeader(&tar.Header{Name: member, Mode: int64(info.Mode().Perm()), Size: int64(len(contents))}); err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Write(contents); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		return seam.Result{}, nil
	}}
}

// runTar inspects the payload produced by the injected tar process.
func runTar(t *testing.T, _ string, arguments ...string) []byte {
	t.Helper()
	if len(arguments) < 2 {
		t.Fatal("missing inspection arguments")
	}
	if arguments[0] == "-xJf" && len(arguments) != 4 {
		t.Fatal("extract needs destination")
	}
	if arguments[0] == "-xOJf" && len(arguments) != 3 {
		t.Fatal("read needs member")
	}
	file, err := os.Open(arguments[1])
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	reader := tar.NewReader(file)
	var output bytes.Buffer
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		contents, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		switch arguments[0] {
		case "-tJf":
			output.WriteString(header.Name + "\n")
		case "-xJf":
			if len(arguments) < 4 {
				t.Fatal("extract needs destination")
				return nil
			}
			writeTestFile(t, filepath.Join(arguments[3], filepath.FromSlash(header.Name)), contents, os.FileMode(header.Mode&0o7777))
		case "-xOJf":
			if len(arguments) < 3 {
				t.Fatal("read needs member")
				return nil
			}
			if header.Name == arguments[2] {
				output.Write(contents)
			}
		default:
			t.Fatalf("unexpected inspection arguments: %q", arguments)
		}
	}
	return output.Bytes()
}

func writeTestFile(t *testing.T, path string, contents []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func assertTestFile(t *testing.T, path string, contents []byte, mode os.FileMode) {
	t.Helper()
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Errorf("close test root: %v", closeErr)
		}
	}()
	got, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, contents) {
		t.Fatalf("%s contents = %q, want %q", path, got, contents)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode&0o111 != 0 && info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("%s is not executable: mode %04o", path, info.Mode().Perm())
	}
}

func assertDistNames(t *testing.T, appDir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(appDir, "dist"))
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dist entries = %#v, want %#v", got, want)
	}
}

// parseTarCreate interprets the injected process's archive request without fixing flag ordering.
func parseTarCreate(t *testing.T, args []string) (archive, directory string, members []string) {
	t.Helper()
	create, xz := false, false
	operands := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if operands {
			members = append(members, arg)
			continue
		}
		if arg == "--" {
			operands = true
			continue
		}
		switch {
		case arg == "-C" || arg == "--directory":
			index++
			if index >= len(args) {
				t.Fatal("tar directory missing")
				return
			}
			directory = args[index]
		case strings.HasPrefix(arg, "--directory="):
			directory = strings.TrimPrefix(arg, "--directory=")
		case arg == "--create":
			create = true
		case arg == "--xz":
			xz = true
		case arg == "--file":
			index++
			if index >= len(args) {
				t.Fatal("tar file missing")
				return
			}
			archive = args[index]
		case strings.HasPrefix(arg, "--file="):
			archive = strings.TrimPrefix(arg, "--file=")
		case strings.HasPrefix(arg, "-"):
			for _, flag := range strings.TrimPrefix(arg, "-") {
				switch flag {
				case 'c':
					create = true
				case 'J':
					xz = true
				case 'f':
					index++
					if index >= len(args) {
						t.Fatal("tar file missing")
						return
					}
					archive = args[index]
				default:
					t.Fatalf("unexpected tar flag %c", flag)
				}
			}
		default:
			members = append(members, arg)
		}
	}
	if !create || !xz || archive == "" || directory == "" {
		t.Fatalf("tar lacks xz creation inputs: %q", args)
	}
	return
}
