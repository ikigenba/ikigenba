package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestGoSourceDoesNotPinLoopbackServicePorts(t *testing.T) {
	// R-QLO8-2HE3
	portLiteral := regexp.MustCompile("127" + `\.` + "0" + `\.` + "0" + `\.` + "1:" + "30" + `[0-9]{2}`)
	var offenders []string

	if err := filepath.Walk("../..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Base(path) == "loopback_literal_test.go" || filepath.Ext(path) != ".go" {
			return nil
		}
		bytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(bytes), "\n") {
			if portLiteral.MatchString(line) {
				offenders = append(offenders, fmt.Sprintf("%s:%d", path, i+1))
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("walk Go source: %v", err)
	}

	if len(offenders) > 0 {
		t.Fatalf("Go source contains hard-coded loopback service ports:\n%s", strings.Join(offenders, "\n"))
	}
}
