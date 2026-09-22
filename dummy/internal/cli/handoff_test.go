package cli

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"testing"

	"github.com/ikigenba/ikigenba/dummy/internal/panel"
	"github.com/ikigenba/ikigenba/dummy/internal/server"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

// R-12WS-SCB1
func TestRunConstructsStoreAfterBindingAndHandsItToPanel(t *testing.T) {
	if reflect.ValueOf(newStore).Pointer() != reflect.ValueOf(widget.NewStore).Pointer() ||
		reflect.ValueOf(panelHandler).Pointer() != reflect.ValueOf(panel.Handler).Pointer() ||
		reflect.ValueOf(serve).Pointer() != reflect.ValueOf(server.Serve).Pointer() || serverHandler != nil {
		t.Fatal("default handoff does not use widget.NewStore, panel.Handler and server.Serve")
	}
	originalServe, originalStore, originalHandler := serve, newStore, panelHandler
	t.Cleanup(func() { serve, newStore, panelHandler = originalServe, originalStore, originalHandler })

	for _, defaultListen := range []bool{false, true} {
		t.Run(strconv.FormatBool(defaultListen), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			store := widget.NewStore()
			handler := http.NewServeMux()
			var events []string
			newStore = func() *widget.Store {
				events = append(events, "store")
				return store
			}
			panelHandler = func(got *widget.Store) http.Handler {
				events = append(events, "handler")
				if got != store {
					t.Error("panel Handler received a different store")
				}
				return handler
			}
			var bound net.Listener
			var address net.Addr
			serve = func(gotCtx context.Context, ln net.Listener, h http.Handler) error {
				events = append(events, "serve")
				if gotCtx != ctx || h != handler {
					t.Errorf("Serve context match %t, handler match %t", gotCtx == ctx, h == handler)
				}
				if defaultListen {
					if _, ok := ln.(*net.TCPListener); !ok {
						t.Errorf("default listener type = %T, want *net.TCPListener", ln)
					}
				} else if ln != bound {
					t.Error("Serve received a different listener")
				}
				if address == nil || ln.Addr().String() != address.String() {
					t.Error("Serve listener differs from the bound address")
				}
				return ln.Close()
			}
			var stdout, stderr bytes.Buffer
			port := "65535"
			process := Process{
				Stdout: &stdout, Stderr: &stderr,
				Listening: func(addr net.Addr) {
					address = addr
					events = append(events, "bound")
				},
			}
			if defaultListen {
				probe, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatalf("probe free port: %v", err)
				}
				port = strconv.Itoa(probe.Addr().(*net.TCPAddr).Port)
				if err = probe.Close(); err != nil {
					t.Fatalf("close port probe: %v", err)
				}
			} else {
				bound = newBlockingListener()
				process.Listen = func(network, addr string) (net.Listener, error) {
					if network != "tcp" || addr != "127.0.0.1:"+port {
						t.Errorf("Listen(%q, %q), want tcp, 127.0.0.1:%s", network, addr, port)
					}
					events = append(events, "listen")
					return bound, nil
				}
			}
			process.LookupEnv = mapLookup(map[string]string{"PORT": port})
			if exit := Run(ctx, process); exit != ExitSuccess {
				t.Fatalf("Run exit = %d; stderr %q", exit, stderr.String())
			}
			want := []string{"bound", "store", "handler", "serve"}
			if !defaultListen {
				want = append([]string{"listen"}, want...)
			} else if address.String() != "127.0.0.1:"+port {
				t.Errorf("default Listen address = %s, want 127.0.0.1:%s", address, port)
			}
			if !reflect.DeepEqual(events, want) {
				t.Errorf("handoff events = %v, want %v", events, want)
			}
			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Errorf("streams = %q, %q, want empty", stdout.String(), stderr.String())
			}
		})
	}
}

// R-12WS-SCB1
func TestRunBuildsNoStoreOrHandlerBeforeSuccessfulBind(t *testing.T) {
	originalServe, originalStore, originalHandler := serve, newStore, panelHandler
	t.Cleanup(func() { serve, newStore, panelHandler = originalServe, originalStore, originalHandler })
	newStore = func() *widget.Store { t.Error("constructed store without successful bind"); return nil }
	panelHandler = func(*widget.Store) http.Handler { t.Error("constructed handler without successful bind"); return nil }
	serve = func(context.Context, net.Listener, http.Handler) error {
		t.Error("served without successful bind")
		return nil
	}
	for _, args := range [][]string{{"--version"}, {"manifest"}, {"--help"}, {"bogus"}, nil} {
		var stdout, stderr bytes.Buffer
		Run(context.Background(), Process{
			Args: args, LookupEnv: mapLookup(map[string]string{"PORT": "65535"}),
			Stdout: &stdout, Stderr: &stderr,
			Listen: func(string, string) (net.Listener, error) { return nil, errors.New("bind failed") },
		})
	}
}
