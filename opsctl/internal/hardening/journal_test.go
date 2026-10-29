package hardening

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func journalCommands(f *fixture) []string {
	var out []string
	for _, command := range f.commands {
		if strings.HasPrefix(command, "systemd-tmpfiles ") || command == "systemctl restart systemd-journald.service" || strings.HasPrefix(command, "journalctl ") {
			out = append(out, command)
		}
	}
	return out
}

func expectedJournalCommands(f *fixture) []string {
	return []string{"systemd-tmpfiles --create --prefix " + filepath.Join(f.root, "var/log/journal"), "systemctl restart systemd-journald.service", "journalctl --flush"}
}

func TestJournalCreationAndExistingEntries(t *testing.T) {
	// R-WPC6-2WYU R-WQK2-GOPJ R-ZH7B-34BK
	for _, entry := range []string{"absent", "directory", "file", "link", "dangling link"} {
		t.Run(entry, func(t *testing.T) {
			f := newFixture(t)
			f.requireApply()
			if err := os.Remove(filepath.Join(f.root, "etc/systemd/journald.conf.d/ikigenba.conf")); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(f.root, "var/log/journal")); err != nil {
				t.Fatal(err)
			}
			switch entry {
			case "absent":
				if err := os.Remove(filepath.Join(f.root, "var/log")); err != nil {
					t.Fatal(err)
				}
			case "directory":
				f.write("var/log/journal/keep", "journal bytes", 0o640)
				f.mkdir("var/log/journal", 0o710)
			case "file":
				f.write("var/log/journal", "existing file", 0o600)
			case "link", "dangling link":
				if entry == "link" {
					f.write("journal-target/keep", "target bytes", 0o600)
					f.mkdir("journal-target", 0o700)
				}
				if err := os.Symlink("../../journal-target", filepath.Join(f.root, "var/log/journal")); err != nil {
					t.Fatal(err)
				}
			}
			previous, err := os.Lstat(filepath.Join(f.root, "var/log/journal"))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			f.commands = nil
			f.hook = func(c host.Command) {
				if c.Name == "systemd-tmpfiles" {
					if f.read("etc/systemd/journald.conf.d/ikigenba.conf") != "[Journal]\nStorage=persistent\n" || f.mode("etc/systemd/journald.conf.d/ikigenba.conf") != 0o644 {
						t.Fatal("journal configuration not installed before tmpfiles")
					}
					if _, err := os.Lstat(c.Args[2]); err != nil {
						t.Fatal(err)
					}
				}
			}
			f.requireApply()
			if got := journalCommands(f); !reflect.DeepEqual(got, expectedJournalCommands(f)) {
				t.Fatalf("journal commands = %q", got)
			}
			after, err := os.Lstat(filepath.Join(f.root, "var/log/journal"))
			if err != nil {
				t.Fatal(err)
			}
			if previous == nil {
				if !after.IsDir() || after.Mode().Perm() != 0o755 || f.mode("var/log") != 0o755 {
					t.Fatal("new journal directory modes")
				}
			} else if !os.SameFile(previous, after) || previous.Mode() != after.Mode() {
				t.Fatal("existing journal entry changed")
			}
			switch entry {
			case "directory":
				if f.read("var/log/journal/keep") != "journal bytes" {
					t.Fatal("journal content changed")
				}
			case "file":
				if f.read("var/log/journal") != "existing file" {
					t.Fatal("journal entry changed")
				}
			case "link", "dangling link":
				if target, err := os.Readlink(filepath.Join(f.root, "var/log/journal")); err != nil || target != "../../journal-target" {
					t.Fatalf("journal link = %q, %v", target, err)
				}
				if entry == "link" && (f.mode("journal-target") != 0o700 || f.read("journal-target/keep") != "target bytes") {
					t.Fatal("journal link target changed")
				}
			}
			f.commands = nil
			changed, err := f.apply()
			if err != nil || changed || len(journalCommands(f)) != 0 {
				t.Fatalf("unchanged journal: changed=%v err=%v commands=%q", changed, err, journalCommands(f))
			}
			f.noTemporary()
		})
	}
}

func TestJournalRetriesEveryFailedCommand(t *testing.T) {
	// R-WQK2-GOPJ R-RV1Q-0TN5 R-ZH7B-34BK
	for index, label := range []string{"systemd-tmpfiles --create", "systemctl restart", "journalctl --flush"} {
		for _, original := range []string{"absent", "old", "correct"} {
			for _, spawn := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/spawn=%v", label, original, spawn), func(t *testing.T) {
					f := newFixture(t)
					f.requireApply()
					name := "etc/systemd/journald.conf.d/ikigenba.conf"
					switch original {
					case "absent":
						if err := os.Remove(filepath.Join(f.root, name)); err != nil {
							t.Fatal(err)
						}
					case "old":
						f.write(name, "[Journal]\nStorage=volatile\n", 0o620)
					case "correct":
						if err := os.Remove(filepath.Join(f.root, "var/log/journal")); err != nil {
							t.Fatal(err)
						}
					}
					f.commands = nil
					f.fail = expectedJournalCommands(f)[index]
					if spawn {
						f.spawnErr = errors.New("fixture could not start process")
					}
					f.hook = func(c host.Command) {
						if c.Name == "systemd-tmpfiles" {
							f.write("var/log/journal/command-output", "keep command effects", 0o600)
							f.mkdir("var/log/journal", 0o750)
						}
					}
					changed, err := f.apply()
					var ce *host.CommandError
					if !errors.As(err, &ce) || ce.Label != label || !reflect.ValueOf(err).Equal(reflect.ValueOf(ce)) || !reflect.DeepEqual(ce.Result, f.fault) || !reflect.ValueOf(ce.Err).Equal(reflect.ValueOf(f.spawnErr)) {
						t.Fatalf("command error = %#v", err)
					}
					if changed != (original == "correct") {
						t.Fatalf("changed = %v for %s", changed, original)
					}
					want := expectedJournalCommands(f)[:index+1]
					if !reflect.DeepEqual(journalCommands(f), want) || f.commands[len(f.commands)-1] != f.fail {
						t.Fatalf("continued after failure: %q", f.commands)
					}
					if original == "old" {
						if f.read(name) != "[Journal]\nStorage=volatile\n" || f.mode(name) != 0o620 {
							t.Fatal("previous journal drop-in not restored")
						}
					} else if _, err := os.Lstat(filepath.Join(f.root, name)); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("drop-in still looks done: %v", err)
					}
					if f.mode("var/log/journal") != 0o750 || f.read("var/log/journal/command-output") != "keep command effects" {
						t.Fatal("journal command effects undone")
					}
					f.noTemporary()
					f.fail, f.spawnErr, f.hook, f.commands = "", nil, nil, nil
					f.requireApply()
					if !reflect.DeepEqual(journalCommands(f), expectedJournalCommands(f)) {
						t.Fatalf("retry commands = %q", journalCommands(f))
					}
				})
			}
		}
	}
}

func TestJournalModeOnlyAndDirectoryOnlyChanges(t *testing.T) {
	// R-WPC6-2WYU R-WQK2-GOPJ R-ZH7B-34BK
	f := newFixture(t)
	f.requireApply()
	name := filepath.Join(f.root, "etc/systemd/journald.conf.d/ikigenba.conf")
	before, err := os.Lstat(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, 0o600); err != nil {
		t.Fatal(err)
	}
	f.commands = nil
	f.requireApply()
	after, err := os.Lstat(name)
	if err != nil || !os.SameFile(before, after) || after.Mode().Perm() != 0o644 || len(journalCommands(f)) != 0 {
		t.Fatalf("mode-only journal change: %v %v %q", after, err, journalCommands(f))
	}
	if err := os.Remove(filepath.Join(f.root, "var/log/journal")); err != nil {
		t.Fatal(err)
	}
	f.commands = nil
	f.requireApply()
	after, err = os.Lstat(name)
	if err != nil || !os.SameFile(before, after) || !reflect.DeepEqual(journalCommands(f), expectedJournalCommands(f)) {
		t.Fatalf("directory-only journal change: %v %v %q", after, err, journalCommands(f))
	}
}

func TestJournalFilesystemFailureRestoresDropIn(t *testing.T) {
	// R-RH3O-4ES3 R-WPC6-2WYU R-ZH7B-34BK
	for _, exists := range []bool{false, true} {
		t.Run(fmt.Sprint(exists), func(t *testing.T) {
			f := newFixture(t)
			f.requireApply()
			name := "etc/systemd/journald.conf.d/ikigenba.conf"
			if exists {
				f.write(name, "old journal configuration", 0o620)
			} else if err := os.Remove(filepath.Join(f.root, name)); err != nil {
				t.Fatal(err)
			}
			if err := os.RemoveAll(filepath.Join(f.root, "var/log")); err != nil {
				t.Fatal(err)
			}
			f.write("var/log", "blocking file", 0o600)
			f.commands = nil
			changed, err := f.apply()
			if changed || err == nil || !strings.Contains(err.Error(), "/var/log") || strings.Contains(err.Error(), f.root) || len(journalCommands(f)) != 0 {
				t.Fatalf("journal filesystem error changed=%v err=%v commands=%q", changed, err, f.commands)
			}
			if exists {
				if f.read(name) != "old journal configuration" || f.mode(name) != 0o620 {
					t.Fatal("journal candidate retained after filesystem error")
				}
			} else if _, err := os.Lstat(filepath.Join(f.root, name)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("journal candidate retained: %v", err)
			}
			if f.read("var/log") != "blocking file" {
				t.Fatal("unrelated blocking file changed")
			}
			f.noTemporary()
		})
	}
}

func TestSSHDSpawnFailuresRestoreAndRetry(t *testing.T) {
	// R-RNQB-Q76Z R-RV1Q-0TN5 R-ZH7B-34BK
	for _, reload := range []bool{false, true} {
		for _, exists := range []bool{false, true} {
			t.Run(fmt.Sprintf("reload=%v/exists=%v", reload, exists), func(t *testing.T) {
				f := newFixture(t)
				f.requireApply()
				name := "etc/ssh/sshd_config.d/10-ikigenba.conf"
				if exists {
					f.write(name, "previous sshd configuration", 0o620)
				} else if err := os.Remove(filepath.Join(f.root, name)); err != nil {
					t.Fatal(err)
				}
				f.commands = nil
				label := "sshd -t"
				f.fail = "sshd -t -f " + filepath.Join(f.root, "etc/ssh/sshd_config")
				if reload {
					label, f.fail = "systemctl reload", "systemctl reload sshd.service"
				}
				f.spawnErr = errors.New("fixture could not start process")
				changed, err := f.apply()
				var ce *host.CommandError
				if changed || !errors.As(err, &ce) || !reflect.ValueOf(err).Equal(reflect.ValueOf(ce)) || ce.Label != label || !reflect.ValueOf(ce.Err).Equal(reflect.ValueOf(f.spawnErr)) || !reflect.DeepEqual(ce.Result, f.fault) || f.commands[len(f.commands)-1] != f.fail {
					t.Fatalf("sshd failure: changed=%v error=%#v commands=%q", changed, err, f.commands)
				}
				if exists {
					if f.read(name) != "previous sshd configuration" || f.mode(name) != 0o620 {
						t.Fatal("previous sshd configuration not restored")
					}
				} else if _, err := os.Lstat(filepath.Join(f.root, name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("sshd candidate retained: %v", err)
				}
				f.noTemporary()
				f.fail, f.spawnErr, f.commands = "", nil, nil
				f.requireApply()
				if !slices.Contains(f.commands, "systemctl reload sshd.service") {
					t.Fatalf("sshd retry did not reload: %q", f.commands)
				}
			})
		}
	}
}

type treeEntry struct {
	mode os.FileMode
	data string
}

func snapshotTree(t *testing.T, root string) map[string]treeEntry {
	t.Helper()
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rootFS.Close() }()
	entries := map[string]treeEntry{}
	err = filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		item := treeEntry{mode: info.Mode()}
		if info.Mode().IsRegular() {
			data, err := rootFS.ReadFile(rel)
			if err != nil {
				return err
			}
			item.data = string(data)
		} else if info.Mode()&os.ModeSymlink != 0 {
			item.data, err = rootFS.Readlink(rel)
			if err != nil {
				return err
			}
		}
		entries[rel] = item
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestApplyWritesOnlyDeclaredLocations(t *testing.T) {
	// R-ZFZE-PCKV
	f := newFixture(t)
	for _, name := range []string{"etc/nginx/nginx.conf", "etc/ikigenba/services", "etc/opt/ikigenba/app/environment", "var/opt/ikigenba/app/data", "var/lib/ikigenba/replication", "run/ikigenba/socket-placeholder", "opt/ikigenba/releases/release/label", "etc/ssh/authorized_keys/alice"} {
		f.write(name, "preserve "+name, 0o640)
	}
	if err := os.Symlink("releases/release", filepath.Join(f.root, "opt/ikigenba/current")); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, f.root)
	f.requireApply()
	after := snapshotTree(t, f.root)
	for _, name := range []string{"etc/systemd/system/user.slice.d", "etc/systemd/system/user-.slice.d", "etc/systemd/journald.conf.d", "var/log", "var/log/journal", "etc/ssh/authorized_keys", "etc/fail2ban/jail.d", "etc/dnf/vars", "etc/systemd/system/user.slice.d/ikigenba.conf", "etc/systemd/system/user-.slice.d/ikigenba.conf", "etc/systemd/journald.conf.d/ikigenba.conf", "etc/ssh/sshd_config.d/10-ikigenba.conf", "etc/fail2ban/jail.d/ikigenba.local", "etc/dnf/vars/releasever", "etc/dnf/automatic.conf", "etc/sudoers.d/ikigenba-accounts"} {
		delete(before, name)
		delete(after, name)
	}
	for name, entry := range before {
		for _, base := range []string{"etc/ikigenba", "etc/opt/ikigenba", "var/opt/ikigenba", "var/lib/ikigenba", "run/ikigenba"} {
			if name == base || strings.HasPrefix(name, base+"/") {
				entry.mode &^= 0o777
				before[name] = entry
				next := after[name]
				next.mode &^= 0o777
				after[name] = next
			}
		}
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("unrelated tree changed:\nbefore: %#v\nafter: %#v", before, after)
	}
	f.noTemporary()
}
