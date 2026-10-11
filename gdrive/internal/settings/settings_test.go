package settings_test

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/gdrive/internal/settings"
)

// R-R1MT-N4D0 R-R2UQ-0W3P
func TestDefaults(t *testing.T) {
	s, err := settings.Read(func(string) (string, bool) { return "", false })
	if err != nil || s != settings.Defaults() || s != (settings.Settings{DrainSeconds: 5, TransferMaxBytes: 1073741824, TransferMaxActive: 2, TransferMaxQueued: 10, TransferKeepDays: 15, TransferKeepCount: 100}) {
		t.Fatalf("%+v %v", s, err)
	}
}

// R-R42M-ENUE R-R5AI-SFL3 R-R6IF-67BS R-R7QB-JZ2H
func TestRead(t *testing.T) {
	names := []string{"DRAIN_SECONDS", "TRANSFER_MAX_BYTES", "TRANSFER_MAX_ACTIVE", "TRANSFER_MAX_QUEUED", "TRANSFER_KEEP_DAYS", "TRANSFER_KEEP_COUNT"}
	units := []string{"seconds", "bytes", "transfers", "transfers", "days", "transfers"}
	for i, name := range names {
		for _, value := range []string{"0", "00", "-1", "+5", "2.5", "5s", "1G", "1e9", "15d", "05", " 5", "5 ", "abc", "٥"} {
			_, err := settings.Read(func(k string) (string, bool) {
				if k == name {
					return value, true
				}
				return "", false
			})
			var e *settings.Error
			if !errors.As(err, &e) || e.Name != name || e.Value != value || e.Unit != units[i] || e.Error() != name+" is '"+value+"', not a positive whole number of "+units[i] {
				t.Fatalf("%s=%q: %v", name, value, err)
			}
		}
		for _, value := range []string{"", "7", "9223372036854775807", "9223372036854775808", "99999999999999999999999"} {
			s, err := settings.Read(func(k string) (string, bool) {
				if k == name {
					return value, true
				}
				return "ignored", false
			})
			if err != nil {
				t.Fatal(err)
			}
			want := settings.Defaults()
			wv := reflect.ValueOf(&want).Elem().Field(i)
			if value != "" {
				n := int64(7)
				if len(value) > 1 {
					n = math.MaxInt64
				}
				wv.SetInt(n)
			}
			if s != want {
				t.Fatalf("%s=%s: %+v want %+v", name, value, s, want)
			}
		}
	}
	_, err := settings.Read(func(k string) (string, bool) {
		if k == "TRANSFER_KEEP_COUNT" {
			return "abc", true
		}
		if k == "TRANSFER_MAX_ACTIVE" {
			return "0", true
		}
		return "", false
	})
	var e *settings.Error
	if !errors.As(err, &e) || e.Name != "TRANSFER_MAX_ACTIVE" {
		t.Fatal(err)
	}
}

// R-R8Y7-XQT6
func TestDrain(t *testing.T) {
	for _, n := range []int64{1, 5, math.MaxInt64 / int64(time.Second), math.MaxInt64} {
		want := time.Duration(math.MaxInt64)
		if n <= math.MaxInt64/int64(time.Second) {
			want = time.Duration(n) * time.Second
		}
		if got := (settings.Settings{DrainSeconds: n}).Drain(); got != want {
			t.Fatalf("%d: %v want %v", n, got, want)
		}
	}
}
