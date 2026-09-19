package cli

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

type nginxConfigReaderFunc func(string) (string, error)

func (f nginxConfigReaderFunc) Get(key string) (string, error) {
	return f(key)
}

func TestNginxApexConfigurationReadFailure(t *testing.T) {
	// R-NUMG-A877
	var keys []string
	reader := nginxConfigReaderFunc(func(key string) (string, error) {
		keys = append(keys, key)
		if key == "host.name" {
			return "SPACE.Example.Test.", nil
		}
		return "", errors.New("read exploded")
	})
	var stderr bytes.Buffer
	hostName, apexApp, code := nginxConfig(reader, &stderr, Deps{Root: "/host"})
	if hostName != "" || apexApp != "" || code != exitFail {
		t.Fatalf("hostName %q apexApp %q exit %d", hostName, apexApp, code)
	}
	if want := "opsctl: config get failed for \"/host/etc/ikigenba/config.json\": read exploded\n"; stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
	if want := []string{"host.name", "host.apex"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("configuration reads = %v, want %v", keys, want)
	}
}
