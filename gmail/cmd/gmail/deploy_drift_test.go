package main

import (
	"os"
	"regexp"
	"registry"
	"strconv"
	"strings"
	"testing"
)

func TestCommittedDeployArtifactPortsAgreeWithRegistry(t *testing.T) {
	// R-9SU9-BYHJ
	wantPort := registry.MustPort("gmail")

	manifest, err := os.ReadFile("../../etc/manifest.env")
	if err != nil {
		t.Fatalf("read committed manifest: %v", err)
	}
	var manifestPorts []string
	for _, line := range strings.Split(string(manifest), "\n") {
		if strings.HasPrefix(line, "PORT=") {
			manifestPorts = append(manifestPorts, strings.TrimPrefix(line, "PORT="))
		}
	}
	if len(manifestPorts) != 1 {
		t.Errorf("committed manifest has %d PORT= lines, want exactly 1", len(manifestPorts))
	} else {
		gotPort, err := strconv.Atoi(manifestPorts[0])
		if err != nil {
			t.Errorf("parse committed manifest PORT=%q: %v", manifestPorts[0], err)
		} else if gotPort != wantPort {
			t.Errorf("committed manifest port = %d, want registry port %d", gotPort, wantPort)
		}
	}

	nginx, err := os.ReadFile("../../etc/nginx.conf")
	if err != nil {
		t.Fatalf("read committed nginx fragment: %v", err)
	}
	loopback := regexp.MustCompile(`127\.0\.0\.1:(\d+)`)
	type occurrence struct {
		line int
		port int
	}
	var occurrences []occurrence
	for lineNumber, line := range strings.Split(string(nginx), "\n") {
		for _, match := range loopback.FindAllStringSubmatch(line, -1) {
			port, err := strconv.Atoi(match[1])
			if err != nil {
				t.Fatalf("parse nginx loopback port on line %d: %v", lineNumber+1, err)
			}
			occurrences = append(occurrences, occurrence{line: lineNumber + 1, port: port})
		}
	}
	if len(occurrences) < 4 {
		t.Errorf("committed nginx fragment has %d loopback-address occurrences, want at least 4", len(occurrences))
	}
	var offenders []string
	for _, found := range occurrences {
		if found.port != wantPort {
			offenders = append(offenders, "line "+strconv.Itoa(found.line)+": port "+strconv.Itoa(found.port))
		}
	}
	if len(offenders) > 0 {
		t.Errorf("committed nginx fragment has ports that differ from registry port %d:\n%s", wantPort, strings.Join(offenders, "\n"))
	}
}
