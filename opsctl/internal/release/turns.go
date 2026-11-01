package release

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HasTurns reports whether a release contains a real turns directory.
func HasTurns(root, sha string) bool {
	if !ValidSHA(sha) {
		return false
	}
	folder, err := resolve(root, ReleasesDir+"/"+sha, ReleasesDir)
	if err != nil {
		return false
	}
	info, err := os.Lstat(filepath.Join(folder, TurnsDir))
	return err == nil && info.IsDir()
}

// LinkTurns atomically replaces the host turns link.
func LinkTurns(root, target string) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("%s: %w", TurnsLink, err)
		}
	}()
	directory, err := makeDirectory(root, "/usr/local/bin", false, false)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(directory, ".turns-*")
	if err != nil {
		return err
	}
	temporary := f.Name()
	defer func() { _ = os.Remove(temporary) }()
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Remove(temporary); err != nil {
		return err
	}
	if err = os.Symlink(target, temporary); err != nil {
		return err
	}
	return os.Rename(temporary, filepath.Join(directory, "turns"))
}

// InstallTurns installs the release's global user units and default space.
func InstallTurns(root, sha, space string) error {
	folder := ReleasesDir + "/" + sha
	if !HasTurns(root, sha) {
		return fmt.Errorf("%s: no turns tree", folder)
	}
	var units [2][]byte
	for i, name := range []string{TurnsService, TurnsTimer} {
		p := folder + "/" + TurnsUnitsPath + "/" + name
		var err error
		units[i], err = readFile(root, p)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	for _, file := range []struct {
		name string
		data []byte
	}{
		{UserUnitDir + "/" + TurnsService, units[0]},
		{UserUnitDir + "/" + TurnsTimer, units[1]},
		{TurnsConfig, []byte("space=" + space + "\n")},
	} {
		directory, err := makeDirectory(root, filepath.Dir(file.name), true, file.name == TurnsConfig)
		if err == nil {
			err = writeTurnsFile(directory, filepath.Base(file.name), file.data)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", file.name, err)
		}
	}
	return nil
}

// makeDirectory resolves ancestors using host-root semantics. The final
// directory may be required to be a real directory rather than a symlink.
func makeDirectory(root, hostPath string, realFinal, resetFinal bool) (string, error) {
	current, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	base := current
	filesystem, err := os.OpenRoot(current)
	if err != nil {
		return "", err
	}
	defer func() { _ = filesystem.Close() }()
	parts := strings.Split(strings.TrimPrefix(hostPath, "/"), "/")
	for i, part := range parts {
		candidate := filepath.Join(current, part)
		info, statErr := os.Lstat(candidate)
		if os.IsNotExist(statErr) {
			relative, e := filepath.Rel(base, candidate)
			if e != nil {
				return "", e
			}
			if err = filesystem.Mkdir(relative, 0o755); err != nil {
				return "", err
			}
			if err = filesystem.Chmod(relative, 0o755); err != nil {
				return "", err
			}
			current = candidate
			continue
		}
		if statErr != nil {
			return "", statErr
		}
		if i == len(parts)-1 && realFinal && !info.IsDir() {
			return "", fmt.Errorf("%s is not a directory", hostPath)
		}
		current, err = Resolve(root, candidate)
		if err != nil {
			return "", err
		}
		info, err = os.Stat(current)
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return "", fmt.Errorf("%s is not a directory", hostPath)
		}
		if i == len(parts)-1 && resetFinal {
			relative, e := filepath.Rel(base, current)
			if e != nil {
				return "", e
			}
			if err = filesystem.Chmod(relative, 0o755); err != nil {
				return "", err
			}
		}
	}
	return current, nil
}

func writeTurnsFile(directory, name string, data []byte) error {
	f, err := os.CreateTemp(directory, ".turns-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Chmod(0o644); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(directory, name))
}
