package sim

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
)

func updateSeedListing(path string, running map[string]string, loops map[string]loop) {
	var temp *os.File
	defer func() {
		if temp != nil {
			_ = temp.Close()
			_ = os.Remove(temp.Name())
		}
	}()
	write := seedWriterFunc(func(line []byte) (int, error) {
		if temp == nil {
			var err error
			temp, err = os.CreateTemp(filepath.Dir(path), ".sandbox-seeds-*")
			if err != nil {
				return 0, err
			}
		}
		return temp.Write(line)
	})
	snapshot := func(id string) machine.Snapshot {
		l, ok := loops[id]
		if !ok {
			return machine.Snapshot{}
		}
		select {
		case <-l.done:
			return machine.Snapshot{}
		default:
			return l.machine.State()
		}
	}
	if err := streamSeedListing(write, running, snapshot); err != nil {
		return
	}
	if temp == nil {
		removeListingFile(path)
		return
	}
	if err := temp.Close(); err != nil {
		return
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) || (err == nil && info.Mode().IsRegular()) {
		if err := os.Rename(temp.Name(), path); err == nil {
			temp = nil
		}
	}
}

type seedWriterFunc func([]byte) (int, error)

func (write seedWriterFunc) Write(line []byte) (int, error) { return write(line) }

func streamSeedListing(w io.Writer, running map[string]string, snapshot func(id string) machine.Snapshot) error {
	ids := make([]string, 0, len(running))
	for id := range running {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int {
		return cmp.Or(cmp.Compare(running[a], running[b]), cmp.Compare(a, b))
	})
	for _, id := range ids {
		if err := writeListingLines(w, running[id], snapshot(id)); err != nil {
			return err
		}
	}
	return nil
}

func writeListingLines(w io.Writer, host string, snapshot machine.Snapshot) error {
	for _, seed := range snapshot.Seeds {
		for _, file := range seed.Files {
			if _, err := fmt.Fprintf(w, "%s %s file %s\n", host, seed.Account, file); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "%s %s commit %s %s %s\n", host, seed.Account, seed.Author, seed.Email, seed.Message); err != nil {
			return err
		}
	}
	for _, home := range snapshot.Homes {
		for _, file := range home.Files {
			if _, err := fmt.Fprintf(w, "%s %s home %s %s\n", host, home.Account, home.User, file); err != nil {
				return err
			}
		}
	}
	return nil
}
