package browser_test

import (
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/oauth/internal/browser"
)

var _ func() browser.Launcher = browser.New

func TestLauncherHasExactExportedInterface(t *testing.T) {
	// R-LXPA-LZMI
	launcherType := reflect.TypeOf((*browser.Launcher)(nil)).Elem()
	if launcherType.Kind() != reflect.Interface {
		t.Fatalf("browser.Launcher kind = %v, want interface", launcherType.Kind())
	}
	if launcherType.Name() != "Launcher" {
		t.Errorf("browser.Launcher type name = %q, want %q", launcherType.Name(), "Launcher")
	}
	if launcherType.PkgPath() != "github.com/ikigenba/ikigenba/oauth/internal/browser" {
		t.Errorf("browser.Launcher package path = %q, want %q", launcherType.PkgPath(), "github.com/ikigenba/ikigenba/oauth/internal/browser")
	}
	if launcherType.NumMethod() != 1 {
		t.Fatalf("browser.Launcher method count = %d, want 1", launcherType.NumMethod())
	}

	method := launcherType.Method(0)
	if method.Name != "Open" {
		t.Errorf("browser.Launcher method name = %q, want %q", method.Name, "Open")
	}
	if method.PkgPath != "" {
		t.Errorf("browser.Launcher.Open package path = %q, want empty path for an exported method", method.PkgPath)
	}
	if method.Type.IsVariadic() {
		t.Errorf("browser.Launcher.Open is variadic, want non-variadic")
	}
	if method.Type.NumIn() != 1 {
		t.Fatalf("browser.Launcher.Open argument count = %d, want 1", method.Type.NumIn())
	}
	if method.Type.In(0).Kind() != reflect.String || method.Type.In(0) != reflect.TypeOf("") {
		t.Errorf("browser.Launcher.Open argument type = %v, want string", method.Type.In(0))
	}
	if method.Type.NumOut() != 1 {
		t.Fatalf("browser.Launcher.Open result count = %d, want 1", method.Type.NumOut())
	}
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if method.Type.Out(0) != errorType {
		t.Errorf("browser.Launcher.Open result type = %v, want built-in error interface %v", method.Type.Out(0), errorType)
	}
	wantMethodType := reflect.TypeOf((func(string) error)(nil))
	if method.Type != wantMethodType {
		t.Errorf("browser.Launcher.Open type = %v, want %v", method.Type, wantMethodType)
	}
}

func TestNewHasExactExportedSignature(t *testing.T) {
	// R-LYX6-ZRD7
	var wantSignature func() browser.Launcher

	gotType := reflect.TypeOf(browser.New)
	wantType := reflect.TypeOf(wantSignature)
	if gotType != wantType {
		t.Fatalf("browser.New signature = %v, want %v", gotType, wantType)
	}
}
