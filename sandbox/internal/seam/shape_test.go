package seam

import (
	"context"
	"io"
	"testing"
)

// R-XZ7Y-OGUJ R-Y0FV-28L8 R-Y43K-7JTB
func TestExactPublicFields(t *testing.T) {
	command := Cmd{"/bin/true", []string{}, t.TempDir()}
	result := Result{[]byte("stdout"), []byte("output"), 0}
	deps := Deps{command.Dir, 1000, func(string) string { return "value" }, Exec, Stream}
	if command.Path != "/bin/true" || len(command.Args) != 0 || command.Dir != deps.Dir || string(result.Stdout) != "stdout" || string(result.Output) != "output" || result.ExitCode != 0 || deps.EUID != 1000 || deps.Getenv("HOME") != "value" {
		t.Fatal("public fields changed")
	}
	if _, err := deps.Exec(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.Stream(context.Background(), command, io.Discard); err != nil {
		t.Fatal(err)
	}
}
