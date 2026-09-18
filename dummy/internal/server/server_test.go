package server

import (
	"context"
	"net"
	"net/http"
	"reflect"
	"testing"
)

// R-B16E-1SRK
func TestServeSignature(t *testing.T) {
	t.Parallel()

	want := reflect.TypeOf((func(context.Context, net.Listener, http.Handler) error)(nil))
	if got := reflect.TypeOf(Serve); got != want {
		t.Errorf("Serve type = %v, want %v", got, want)
	}
}
