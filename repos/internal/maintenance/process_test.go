package maintenance_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/repos/internal/maintenance"
)

func TestRunningGitEndsBeforeCycleOrStopReturns(t *testing.T) {
	// R-H55V-K9GY R-GRQZ-CSBB R-H0AA-16I6 R-H1I6-EY8V R-IIUM-PSSC R-1B8Y-PDRY
	for _, mode := range []string{"cycle", "stop", "operation"} {
		t.Run(mode, func(t *testing.T) {
			f := fixture(t, 1)
			id := f.repos[0].ID
			// A fixture config FIFO makes the real git block reading configuration.
			// Opening its writer proves git has opened the reader, without a sleep or
			// substituting another executable. Keeping it open supplies no config.
			config := filepath.Join(f.st.Dir(id), "config")
			if err := os.Remove(config); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(config, 0600); err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(f.st.Dir(id))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := root.Close(); err != nil {
					t.Error(err)
				}
			}()
			writers := make(chan *os.File, 1)
			openErrors := make(chan error, 1)
			go func() {
				file, err := root.OpenFile("config", os.O_WRONLY, 0)
				if err != nil {
					openErrors <- err
					return
				}
				writers <- file
			}()
			ctx, cancel := context.WithCancel(maintenanceContext(t))
			defer cancel()
			var done <-chan struct{}
			var schedule *maintenance.Scheduler
			if mode == "stop" {
				schedule = maintenance.Start(f.cfg)
				timer(t, f.clock, time.Hour).fire <- f.clock.Now()
				timer(t, f.clock, time.Hour)
			} else {
				done = runCycle(ctx, f.cfg)
			}
			deadline := timer(t, f.clock, 23*time.Second)
			var writer *os.File
			select {
			case writer = <-writers:
			case err := <-openErrors:
				t.Fatal(err)
			case <-testContext(t).Done():
				t.Fatal("git did not open FIFO")
			}
			defer func() {
				if err := writer.Close(); err != nil {
					t.Error(err)
				}
			}()
			if !f.lim.Busy(id) || f.lim.Pressure().Write.Active != 1 {
				t.Fatal("repository not busy while git running")
			}
			if mode == "stop" {
				stopCtx, stopCancel := context.WithCancel(maintenanceContext(t))
				stopCancel()
				schedule.Stop(stopCtx)
			} else {
				if mode == "operation" {
					deadline.fire <- f.clock.Now()
				} else {
					cancel()
				}
				receive(t, done)
			}
			if _, err := writer.Write([]byte("reader must be gone")); !errors.Is(err, syscall.EPIPE) {
				t.Fatalf("git reader survives completed maintenance: %v", err)
			}
			es := repoEvents(t, f, id)
			if mode == "operation" {
				if len(es) != 1 || es[0].Name != "operation.timed_out" || es[0].Attrs["limit"] != "operation_seconds" || es[0].Attrs["operation"] != "maintenance" || es[0].RequestID != "" || es[0].User != "" {
					t.Fatalf("timeout events: %+v", es)
				}
			} else if len(es) != 0 {
				t.Fatalf("cancellation events: %+v", es)
			}
			assertIdle(t, f)
		})
	}
}
