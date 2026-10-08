package apps_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func releaseLifecycleRoot(t *testing.T, label string) (string, string) {
	t.Helper()
	root := lifecycleRoot(t)
	sha := strings.Repeat("c", 40)
	directory := filepath.Join(root, "opt/ikigenba/releases", sha)
	if err := os.MkdirAll(directory, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "opt/notes"), filepath.Join(directory, "notes")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("releases/"+sha, filepath.Join(root, "opt/ikigenba/current")); err != nil {
		t.Fatal(err)
	}
	if label != "" {
		writeFixturePath(t, root, "opt/ikigenba/releases/"+sha+"/label", label+"\n")
	}
	writeFixturePath(t, root, "etc/opt/ikigenba/notes/env", "unchanged configuration")
	writeFixturePath(t, root, "var/opt/ikigenba/retired/state/keep", "saved state")
	return root, sha
}

func TestReleasedLifecycleUsesReleaseIdentityAndPreservesFiles(t *testing.T) {
	// R-GO3R-M73B R-GPBN-ZYU0 R-F58A-EIQ2 R-EXWW-3W9W R-F40E-0QZD
	for _, action := range []string{"restart", "disable", "enable", "disabled restart"} {
		t.Run(action, func(t *testing.T) {
			root, sha := releaseLifecycleRoot(t, "candidate")
			model := &lifecycleModel{socketActive: true, serviceActive: true, socketEnabled: true, serviceEnabled: true, version: "must not query binary"}
			if action == "enable" || action == "disabled restart" {
				model.socketActive = false
				model.serviceActive = false
				model.socketEnabled = false
				model.serviceEnabled = false
			}
			env := host.Env{Root: root, Execute: model.execute}
			before := snapshotTree(t, root)
			switch action {
			case "restart", "disabled restart":
				report, err := apps.Restart(t.Context(), env, "notes")
				state := "active"
				if action == "disabled restart" {
					state = "disabled"
				}
				if err != nil || report != (apps.ServiceReport{Name: "notes", Version: sha[:7], State: state}) {
					t.Fatalf("restart %v %v", report, err)
				}
			case "disable":
				if err := apps.Disable(t.Context(), env, "notes", model.hooks()); err != nil {
					t.Fatal(err)
				}
			case "enable":
				if err := apps.Enable(t.Context(), env, "notes", model.hooks()); err != nil {
					t.Fatal(err)
				}
				if len(model.reports) != 2 || model.reports[1] != (uninstallReport{step: "service", detail: "notes " + sha[:7] + " active", success: true}) {
					t.Fatalf("reports %v", model.reports)
				}
			}
			for _, command := range model.commands {
				if command.Name != "systemctl" {
					t.Fatalf("version binary or other process queried: %v", command)
				}
				if action == "disabled restart" && len(command.Args) > 0 && command.Args[0] != "show" {
					t.Fatalf("disabled command %v", command)
				}
			}
			if action == "disable" || action == "enable" {
				if len(model.configure) != 1 || model.configure[0].App != "notes" {
					t.Fatalf("manifest %v", model.configure)
				}
			}
			if !reflect.DeepEqual(before, snapshotTree(t, root)) {
				t.Fatal("lifecycle changed release/package/configuration files")
			}
		})
	}
}

func TestReleasedLifecycleDataOnlyAndLayoutErrorsBeforeCommands(t *testing.T) {
	// R-EMXS-NYLN R-F40E-0QZD
	for _, action := range []string{"restart", "disable", "enable"} {
		for _, condition := range []string{"data only", "not service", "broken current", "missing binary", "missing manifest", "missing unit"} {
			t.Run(action+" "+condition, func(t *testing.T) {
				root, _ := releaseLifecycleRoot(t, "")
				app := "notes"
				message := ""
				switch condition {
				case "data only":
					app = "retired"
					message = "retired is not in the current release"
				case "not service":
					app = "absent"
					message = "no service 'absent'"
				case "broken current":
					if err := os.Remove(filepath.Join(root, "opt/ikigenba/current")); err != nil {
						t.Fatal(err)
					}
					writeFixturePath(t, root, "opt/ikigenba/current", "invalid")
					message = "/opt/ikigenba/current does not name a release"
				case "missing binary":
					removeFixturePath(t, root, "opt/ikigenba/releases/"+strings.Repeat("c", 40)+"/notes/bin/notes")
					message = "no service 'notes'"
				case "missing manifest":
					if action == "restart" {
						return
					}
					removeFixturePath(t, root, "opt/ikigenba/releases/"+strings.Repeat("c", 40)+"/notes/etc/manifest.toml")
				case "missing unit":
					if action == "restart" {
						return
					}
					removeFixturePath(t, root, "etc/systemd/system/ikigenba-notes.service")
					message = "notes is not installed"
				}
				before := snapshotTree(t, root)
				reports := []uninstallReport{}
				hooks := apps.LifecycleHooks{Report: func(step, detail string, success bool) error {
					reports = append(reports, uninstallReport{step, detail, success})
					return nil
				}, Configure: func(context.Context, apps.Manifest) error { t.Fatal("configured prerequisites failure"); return nil }}
				env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
					t.Fatal("executed prerequisites failure")
					return host.Result{}, nil
				}}
				var err error
				switch action {
				case "restart":
					_, err = apps.Restart(t.Context(), env, app)
				case "disable":
					err = apps.Disable(t.Context(), env, app, hooks)
				case "enable":
					err = apps.Enable(t.Context(), env, app, hooks)
				}
				var failure *apps.LifecycleError
				if !errors.As(err, &failure) || failure.Code != 1 || (message != "" && failure.Message != message) {
					t.Fatalf("failure %v", err)
				}
				if action != "restart" {
					stage := "stop"
					if action == "enable" {
						stage = "enable"
					}
					if !reflect.DeepEqual(reports, []uninstallReport{{stage, failure.Message, false}}) {
						t.Fatalf("reports %v", reports)
					}
				}
				if !reflect.DeepEqual(before, snapshotTree(t, root)) {
					t.Fatal("changed prerequisites failure")
				}
			})
		}
	}
}

func TestReleasedStatusLabelDataOnlyAndNoBinaryQueries(t *testing.T) {
	// R-F0CO-VFRA R-F1KL-97HZ R-GO3R-M73B
	for _, label := range []string{"candidate", ""} {
		t.Run(label, func(t *testing.T) {
			root, sha := releaseLifecycleRoot(t, label)
			model := &lifecycleModel{socketActive: true, serviceActive: true, socketEnabled: true, serviceEnabled: true}
			before := snapshotTree(t, root)
			rows, err := apps.Status(t.Context(), host.Env{Root: root, Execute: func(ctx context.Context, c host.Command) (host.Result, error) {
				if c.Name != "systemctl" || !strings.Contains(c.Args[len(c.Args)-1], "notes") {
					t.Fatalf("unexpected query %v", c)
				}
				return model.execute(ctx, c)
			}})
			wantLabel := label
			if label == "" {
				wantLabel = "-"
			}
			want := []apps.StatusRow{{Name: "notes", Version: sha[:7], Label: wantLabel, State: "active", Socket: "active", JournalMode: "-"}, {Name: "retired", Version: "-", Label: "-", State: "-", Socket: "-", JournalMode: "-"}}
			if err != nil || !reflect.DeepEqual(rows, want) {
				t.Fatalf("status %v %v", rows, err)
			}
			if len(model.commands) != 2 {
				t.Fatalf("queries %v", model.commands)
			}
			if !reflect.DeepEqual(before, snapshotTree(t, root)) {
				t.Fatal("status changed files")
			}
			if err := os.Remove(filepath.Join(root, "opt/ikigenba/current")); err != nil {
				t.Fatal(err)
			}
			writeFixturePath(t, root, "opt/ikigenba/current", "not a link")
			_, err = apps.Status(t.Context(), host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
				t.Fatal("queried broken layout")
				return host.Result{}, nil
			}})
			if err == nil || err.Error() != "/opt/ikigenba/current does not name a release" {
				t.Fatalf("layout error %v", err)
			}
		})
	}
}
