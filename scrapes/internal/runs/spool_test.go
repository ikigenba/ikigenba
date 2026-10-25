package runs_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/scrapes/internal/source"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
)

// R-M5IX-3WJ1
func TestWalkBuildSpool(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "source failure"
		}
		t.Run(name, func(t *testing.T) {
			type observation struct {
				path string
				err  error
			}
			observed := make(chan observation, 1)
			release := make(chan struct{})
			sc := fake{name: "source", scrape: func(ctx context.Context, batch func([]source.Row) error) error {
				spool := source.SpoolFrom(ctx)
				if spool == nil {
					observed <- observation{err: errors.New("walk has no spool")}
					return errors.New("missing spool")
				}
				f, e := spool()
				if e != nil {
					observed <- observation{err: e}
					return e
				}
				defer func() { _ = f.Close() }()
				info, e := f.Stat()
				if e == nil && (!info.Mode().IsRegular() || info.Size() != 0) {
					e = errors.New("spool is not an empty regular file")
				}
				if e == nil {
					_, e = f.WriteString("source archive")
				}
				observed <- observation{path: f.Name(), err: e}
				<-release
				if e != nil {
					return e
				}
				if fail {
					return errors.New("source refused")
				}
				return batch([]source.Row{{SourceID: "filing"}})
			}}
			f := setup(t, sc)
			r, e := f.e.Start(context.Background(), caller(), "source")
			if e != nil {
				close(release)
				t.Fatal(e)
			}
			o := receive(t, observed)
			// Always let the walk finish before asserting observations.
			close(release)
			if o.err != nil {
				t.Fatal(o.err)
			}
			if filepath.Dir(o.path) != f.dir || !strings.HasPrefix(filepath.Base(o.path), r.ID+".") || filepath.Base(o.path) == r.ID+".db" {
				t.Fatal(o.path, r.ID)
			}
			endedRun := ended(t, f.st, r.ID)
			if fail {
				if endedRun.Status != store.StatusFailed || endedRun.Reason != store.ReasonSource {
					t.Fatal(endedRun)
				}
			} else if endedRun.Status != store.StatusDone || endedRun.Rows != 1 {
				t.Fatal(endedRun)
			}
			entries, e := os.ReadDir(f.dir)
			if e != nil {
				t.Fatal(e)
			}
			if fail {
				if len(entries) != 0 {
					t.Fatal(entries)
				}
			} else if len(entries) != 1 || entries[0].Name() != r.ID+".db" || !entries[0].Type().IsRegular() {
				t.Fatal(entries)
			}
		})
	}
}
