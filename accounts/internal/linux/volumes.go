package linux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

var (
	lsblkPair = regexp.MustCompile(`([A-Z]+)="([^"]*)"`)
	diskValue = regexp.MustCompile(`^[A-Za-z0-9_.+-]+$`)
)

func lsblkRow(output []byte, key, value string) map[string]string {
	for line := range strings.SplitSeq(string(output), "\n") {
		fields := make(map[string]string)
		for _, pair := range lsblkPair.FindAllStringSubmatch(line, -1) {
			fields[pair[1]] = pair[2]
		}
		if fields[key] == value {
			return fields
		}
	}
	return nil
}

func (h *Host) growRoot(ctx context.Context) error {
	result, err := h.run(ctx, "lsblk", "--pairs", "--noheadings", "--output", "NAME,PKNAME,FSTYPE,MOUNTPOINT")
	if err != nil {
		return err
	}
	row := lsblkRow(result.Stdout, "MOUNTPOINT", "/")
	name, parent := row["NAME"], row["PKNAME"]
	if !deviceName(name) || (parent != "" && !deviceName(parent)) {
		return fmt.Errorf("lsblk: no valid device for /")
	}
	disk := name
	if parent != "" {
		disk = parent
	}
	device := h.path("/dev/" + disk)
	size, err := h.blockSize(ctx, device)
	if err != nil || size == h.rootSize {
		return err
	}
	if parent != "" {
		path := "/sys/class/block/" + name + "/partition"
		input, err := h.read(path)
		if err != nil {
			return err
		}
		number := strings.TrimSpace(string(input))
		partition, err := strconv.ParseUint(number, 10, 32)
		if err != nil || partition == 0 {
			return fmt.Errorf("%s: invalid partition number", path)
		}
		if _, err := h.query(ctx, Command{Name: "growpart", Args: []string{device, number}}, 0, 1); err != nil {
			return err
		}
	}
	if err := h.growFilesystem(ctx, row["FSTYPE"], h.path("/dev/"+name), h.path("/")); err != nil {
		return err
	}
	h.rootSize = size
	return nil
}

func deviceName(name string) bool {
	return diskValue.MatchString(name) && name != "." && name != ".."
}

func (h *Host) blockSize(ctx context.Context, device string) (string, error) {
	result, err := h.run(ctx, "blockdev", "--getsize64", device)
	if err != nil {
		return "", err
	}
	size := strings.TrimSpace(string(result.Stdout))
	if _, err := strconv.ParseUint(size, 10, 64); err != nil {
		return "", fmt.Errorf("blockdev: invalid size for %s: %w", device, err)
	}
	return size, nil
}

func (h *Host) growFilesystem(ctx context.Context, kind, device, mount string) error {
	switch kind {
	case "xfs":
		_, err := h.run(ctx, "xfs_growfs", "-d", mount)
		return err
	case "ext4":
		_, err := h.run(ctx, "resize2fs", device)
		return err
	}
	return nil
}

var dataMountUnits = [...]string{"data.mount", "home.mount", "srv.mount"}

func (h *Host) data(ctx context.Context, state protocol.DesiredState) (bool, error) {
	name, err := h.dataDevice(ctx, state.DataVolume)
	if err != nil {
		return false, err
	}
	if name == "" {
		if len(state.Accounts) != 0 || len(state.Users) != 0 {
			return false, machine.ErrNoDataVolume
		}
		return false, nil
	}
	device := h.path("/dev/" + name)
	uuid, kind, err := h.filesystem(ctx, device)
	if err != nil {
		return false, err
	}
	if err := h.dataMounts(ctx, uuid, kind); err != nil {
		return false, err
	}
	size, err := h.blockSize(ctx, device)
	if err != nil {
		return false, err
	}
	if state.DataVolume != h.dataVolumeID || size != h.dataSize {
		if err := h.growFilesystem(ctx, kind, device, h.path(DataDir)); err != nil {
			return false, err
		}
		h.dataVolumeID, h.dataSize = state.DataVolume, size
	}
	return true, nil
}

func (h *Host) dataDevice(ctx context.Context, volume string) (string, error) {
	result, err := h.run(ctx, "lsblk", "--nodeps", "--pairs", "--noheadings", "--output", "NAME,SERIAL")
	if err != nil {
		return "", err
	}
	if volume == "" {
		return "", nil
	}
	row := lsblkRow(result.Stdout, "SERIAL", strings.Replace(volume, "-", "", 1))
	if row == nil {
		return "", nil
	}
	name := row["NAME"]
	if !deviceName(name) {
		return "", fmt.Errorf("lsblk: no valid device for %s", volume)
	}
	return name, nil
}

func (h *Host) filesystem(ctx context.Context, device string) (uuid, kind string, err error) {
	probe := Command{Name: "blkid", Args: []string{"-p", "--output", "export", device}}
	result, err := h.query(ctx, probe, 0, 2)
	if err != nil {
		return "", "", err
	}
	if result.ExitCode == 2 {
		if _, err := h.run(ctx, "mkfs.xfs", "-L", "ikigenbadata", device); err != nil {
			return "", "", err
		}
		result, err = h.query(ctx, probe, 0)
		if err != nil {
			return "", "", err
		}
	}
	for line := range strings.SplitSeq(string(result.Stdout), "\n") {
		key, value, _ := strings.Cut(line, "=")
		switch key {
		case "UUID":
			uuid = value
		case "TYPE":
			kind = value
		}
	}
	if !deviceName(uuid) || !diskValue.MatchString(kind) {
		return "", "", fmt.Errorf("blkid: missing or invalid filesystem UUID or type for %s", device)
	}
	return uuid, kind, nil
}

const dataMountContents = "[Mount]\nWhat=/dev/disk/by-uuid/%s\nWhere=/data\nType=%s\n\n[Install]\nWantedBy=local-fs.target\n"
const bindMountContents = "[Unit]\nRequires=data.mount\nAfter=data.mount\n\n[Mount]\nWhat=/data/%s\nWhere=/%s\nType=none\nOptions=bind\n\n[Install]\nWantedBy=local-fs.target\n"

func (h *Host) dataMounts(ctx context.Context, uuid, kind string) error {
	contents := [...]string{
		fmt.Sprintf(dataMountContents, uuid, kind),
		fmt.Sprintf(bindMountContents, "home", "home"),
		fmt.Sprintf(bindMountContents, "srv", "srv"),
	}
	changed := false
	for i, unit := range dataMountUnits {
		file, err := h.write(unitDirectory+unit, []byte(contents[i]), 0o644)
		if err != nil {
			return err
		}
		changed = changed || file.changed
	}
	if changed {
		if _, err := h.run(ctx, "systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	if err := h.enableAndStart(ctx, dataMountUnits[0]); err != nil {
		return err
	}
	for _, dir := range []struct {
		path string
		mode fs.FileMode
	}{{DataDir + "/home", 0o755}, {DataDir + "/srv", 0o755}, {HostKeyDir, 0o700}} {
		if _, err := h.dir(dir.path, dir.mode); err != nil {
			return err
		}
	}
	for _, unit := range dataMountUnits[1:] {
		if err := h.enableAndStart(ctx, unit); err != nil {
			return err
		}
	}
	return nil
}

const hostKeyName = "ssh_host_ed25519_key"
const privateHostKey = HostKeyDir + "/" + hostKeyName
const publicHostKey = privateHostKey + ".pub"

func (h *Host) hostKey(_ context.Context) error {
	paths := [...]string{privateHostKey, publicHostKey}
	source := [...]string{"/etc/ssh/" + hostKeyName, "/etc/ssh/" + hostKeyName + ".pub"}
	modes := [...]fs.FileMode{0o600, 0o644}
	var missing [2]bool
	for i, path := range paths {
		info, err := os.Lstat(h.path(path))
		if errors.Is(err, fs.ErrNotExist) {
			missing[i] = true
			continue
		}
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s: expected regular file", path)
		}
	}
	if !missing[0] && !missing[1] {
		for i, path := range paths {
			if err := h.mode(path, modes[i]); err != nil {
				return err
			}
		}
		return nil
	}
	var contents [2]*os.File
	for i, path := range source {
		info, err := os.Lstat(h.path(path))
		if err != nil {
			return hostError(path, err)
		}
		if !info.Mode().IsRegular() {
			return hostError(path, fs.ErrInvalid)
		}
		input, err := os.Open(h.path(path))
		if err != nil {
			return hostError(path, err)
		}
		defer func() { _ = input.Close() }()
		contents[i] = input
	}
	// Replace an existing half before filling the missing half. A failure
	// cannot leave two apparently complete files belonging to different keys.
	order := [...]int{0, 1}
	if missing[0] && !missing[1] {
		order = [...]int{1, 0}
	}
	for _, i := range order {
		if err := h.copyHostKey(paths[i], contents[i], modes[i]); err != nil {
			return err
		}
	}
	return nil
}

func (h *Host) copyHostKey(path string, source *os.File, mode fs.FileMode) error {
	return h.writeStream(path, mode, func(output io.Writer) (bool, error) {
		equal, err := matchesKey(source, h.path(path))
		if err != nil || equal {
			return false, err
		}
		if _, err := source.Seek(0, io.SeekStart); err != nil {
			return false, err
		}
		_, err = io.Copy(output, source)
		return true, err
	})
}

func matchesKey(source io.Reader, existing string) (bool, error) {
	key, err := os.Open(filepath.Clean(existing))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = key.Close() }()
	var fromSource, fromKey [4096]byte
	for {
		sourceN, sourceErr := io.ReadFull(source, fromSource[:])
		keyN, keyErr := io.ReadFull(key, fromKey[:])
		for _, err := range []error{sourceErr, keyErr} {
			if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				return false, err
			}
		}
		if sourceN != keyN || !bytes.Equal(fromSource[:sourceN], fromKey[:keyN]) {
			return false, nil
		}
		if sourceErr != nil {
			return true, nil
		}
	}
}
