package apps

import (
	"fmt"
	"math/big"
	"os"
	"path"
	"strconv"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

// WriteReleaseUnits publishes the socket and release-aware service units.
func WriteReleaseUnits(env host.Env, app string, m Manifest, t Timeouts) error {
	if err := ValidateName(app); err != nil {
		return err
	}

	socket := socketUnitName(app)
	service := "[Unit]\nDescription=Ikigenba " + app + " app\nRequires=" + socket + "\nAfter=" + socket + " " + ServicesUnit + "\nWants=" + ServicesUnit + "\n\n" +
		"[Service]\nType=notify\nExecStart=" + rootedHostPath(env.Root, release.CurrentLink, app, "bin", app) + "\n" +
		"WorkingDirectory=" + rootedHostPath(env.Root, DataRoot, app) + "\nEnvironmentFile=" + rootedHostPath(env.Root, EnvRoot, app, "env") + "\n" +
		"User=ikigenba\nRuntimeDirectory=ikigenba/" + app + "\nPrivateTmp=yes\nRestart=on-failure\nTimeoutStopSec=" + strconv.FormatInt(t.StopSeconds, 10) + "\n" +
		resourceUnitLines(m.Resources) + "\n[Install]\nWantedBy=multi-user.target\n"

	return writeReleaseUnitFiles(env, []releaseUnitFile{{socketUnitName(app), socketUnitBytes(env.Root, app)}, {appUnitName(app), []byte(service)}})
}

// WriteServicesUnit publishes the boot-time services file writer.
func WriteServicesUnit(env host.Env) error {
	data := "[Unit]\nDescription=Ikigenba services file\n\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=" + rootedHostPath(env.Root, release.OpsctlLink) + " services apply\n\n[Install]\nWantedBy=multi-user.target\n"
	return writeReleaseUnitFiles(env, []releaseUnitFile{{ServicesUnit, []byte(data)}})
}

type releaseUnitFile struct {
	name string
	data []byte
}

func writeReleaseUnitFiles(env host.Env, files []releaseUnitFile) error {
	fs, err := os.OpenRoot(env.Root)
	if err != nil {
		return err
	}
	defer func() { _ = fs.Close() }()
	directory := "etc/systemd/system"
	for _, name := range []string{"etc", "etc/systemd", directory} {
		info, statErr := fs.Lstat(name)
		if statErr == nil {
			if !info.IsDir() {
				return fmt.Errorf("%s is not a directory", name)
			}
			continue
		}
		if !os.IsNotExist(statErr) {
			return statErr
		}
		if err := fs.Mkdir(name, 0o755); err != nil {
			return err
		}
		if err := fs.Chmod(name, 0o755); err != nil {
			return err
		}
	}
	for _, file := range files {
		if err := publishAtomicFile(fs, directory, path.Join(directory, file.name), file.data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// CheckReleaseResources validates slice ceilings against exactly these manifests.
func CheckReleaseResources(root string, manifests []Manifest) (string, error) {
	suite, err := readInstallSlice(root, "ikigenba.slice", true)
	if err != nil {
		return "", err
	}
	appsCeiling := int64(0)
	all, apps := big.NewInt(134217728), new(big.Int)
	for _, m := range manifests {
		resources := m.Resources
		if resources.Slice == "" {
			resources = defaultResources()
		}
		unit := "ikigenba-" + resources.Slice + ".slice"
		ceiling, err := readInstallSlice(root, unit, resources.Slice == "apps")
		if err != nil {
			return "", err
		}
		if resources.Slice == "core" {
			ceiling = suite
			unit = "ikigenba.slice"
		} else {
			appsCeiling = ceiling
			apps.Add(apps, big.NewInt(resources.MemoryMax))
		}
		if resources.MemoryMax > ceiling {
			return "", fmt.Errorf("%s: etc/manifest.toml: memory_max %s is more than %s's MemoryMax %s", m.App, renderMemory(big.NewInt(resources.MemoryMax)), unit, renderMemory(big.NewInt(ceiling)))
		}
		all.Add(all, big.NewInt(resources.MemoryMax))
	}
	warning := func(unit string, sum *big.Int, ceiling int64) string {
		if sum.Cmp(new(big.Int).Mul(big.NewInt(ceiling), big.NewInt(2))) <= 0 {
			return ""
		}
		return fmt.Sprintf("; warning: %s memory_max adds up to %s, more than twice its MemoryMax %s", unit, renderMemory(sum), renderMemory(big.NewInt(ceiling)))
	}
	result := ""
	if appsCeiling > 0 {
		result = warning("ikigenba-apps.slice", apps, appsCeiling)
	}
	return result + warning("ikigenba.slice", all, suite), nil
}
