// Package protocol defines the wire contract between accounts and accountsd.
package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// The daemon fleet uses these paths, query names, and media types.
const (
	BinaryPath    = "/accountsd"
	StatePath     = "/accountsd/v1/state"
	ReportPath    = "/accountsd/v1/report"
	QueryAfter    = "after"
	QueryDigest   = "digest"
	ContentJSON   = "application/json"
	ContentBinary = "application/octet-stream"
	DigestPrefix  = "sha256:"
)

// Wire version, payload bounds, and polling interval.
const (
	Version        = 1
	MaxStateBytes  = 2 << 20
	MaxReportBytes = 256 << 10
	MaxBinaryBytes = 64 << 20
	MaxErrorBytes  = 4096
	MaxFailedUnits = 100
	MaxSlices      = 50
	MaxSignIns     = 200
	DigestBuffer   = 32 << 10
	LongPoll       = 45 * time.Second
)

// Account and user states carried over the wire.
const (
	AccountActive  = "active"
	AccountRemoved = "removed"
	UserActive     = "active"
	UserLocked     = "locked"
	UserDeleted    = "deleted"
)

// Decode errors let callers distinguish invalid payloads and versions.
var (
	ErrTooLarge  = errors.New("protocol document exceeds its size limit")
	ErrMalformed = errors.New("malformed protocol document")
	ErrVersion   = errors.New("unsupported protocol version")
)

// DesiredState contains the configuration the host should apply.
type DesiredState struct {
	Protocol   int       `json:"protocol"`
	Host       string    `json:"host"`
	Version    int64     `json:"version"`
	Daemon     string    `json:"daemon"`
	DataVolume string    `json:"data_volume"`
	RootGB     int       `json:"root_gb"`
	DataGB     int       `json:"data_gb"`
	Accounts   []Account `json:"accounts"`
	Users      []User    `json:"users"`
}

// Account describes a group and its service account.
type Account struct {
	Slug  string `json:"slug"`
	GID   int64  `json:"gid"`
	State string `json:"state"`
}

// User describes an SSH user and their authorized keys.
type User struct {
	Name    string   `json:"name"`
	Account string   `json:"account"`
	UID     int64    `json:"uid"`
	State   string   `json:"state"`
	Keys    []string `json:"keys"`
}

// Report captures the results of applying state and the health of the host.
type Report struct {
	Protocol        int        `json:"protocol"`
	Attempted       int64      `json:"attempted"`
	Applied         int64      `json:"applied"`
	Error           string     `json:"error"`
	InstanceID      string     `json:"instance_id"`
	Type            string     `json:"type"`
	AMI             string     `json:"ami"`
	OS              string     `json:"os"`
	Kernel          string     `json:"kernel"`
	Booted          time.Time  `json:"booted"`
	DaemonVersion   string     `json:"daemon_version"`
	Load            [3]float64 `json:"load"`
	CPUPercent      float64    `json:"cpu_percent"`
	MemoryUsed      int64      `json:"memory_used"`
	MemoryAvailable int64      `json:"memory_available"`
	SwapUsed        int64      `json:"swap_used"`
	SwapTotal       int64      `json:"swap_total"`
	Root            Disk       `json:"root"`
	Data            Disk       `json:"data"`
	Sessions        int        `json:"sessions"`
	Updates         int        `json:"updates"`
	Reboot          bool       `json:"reboot"`
	FailedUnits     []string   `json:"failed_units"`
	Slices          []Slice    `json:"slices"`
	SignIns         []SignIn   `json:"sign_ins"`
}

// Disk reports filesystem usage.
type Disk struct {
	UsedBytes   int64 `json:"used_bytes"`
	SizeBytes   int64 `json:"size_bytes"`
	InodesUsed  int64 `json:"inodes_used"`
	InodesTotal int64 `json:"inodes_total"`
}

// Slice reports resource usage for an account.
type Slice struct {
	Account     string  `json:"account"`
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryBytes int64   `json:"memory_bytes"`
}

// SignIn records the most recent sign-in for a user.
type SignIn struct {
	User string    `json:"user"`
	At   time.Time `json:"at"`
}

// DecodeState reads one bounded v1 desired state document.
func DecodeState(r io.Reader) (DesiredState, error) {
	var state DesiredState
	if err := decode(r, MaxStateBytes, &state); err != nil {
		return DesiredState{}, err
	}
	if state.Protocol != Version {
		return DesiredState{}, ErrVersion
	}
	if state.Host == "" || state.Version < 1 {
		return DesiredState{}, ErrMalformed
	}
	return state, nil
}

// DecodeReport reads one bounded v1 report and normalizes its lists.
func DecodeReport(r io.Reader) (Report, error) {
	var report Report
	if err := decode(r, MaxReportBytes, &report); err != nil {
		return Report{}, err
	}
	if report.Protocol != Version {
		return Report{}, ErrVersion
	}
	return report.Bounded(), nil
}

func decode(r io.Reader, limit int64, dst any) error {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if int64(len(body)) > limit {
		return ErrTooLarge
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	body = bytes.TrimLeft(body, " \t\r\n")
	if len(body) == 0 || body[0] != '{' {
		return ErrMalformed
	}
	if err := decodeValue(body, reflect.ValueOf(dst).Elem()); err != nil {
		return fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	return nil
}

var jsonUnmarshaler = reflect.TypeFor[json.Unmarshaler]()

// Decode each declared member once so that the last occurrence replaces the
// previous value, including when the last one is null or an object.
func decodeValue(raw []byte, dst reflect.Value) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		dst.SetZero()
		return nil
	}
	if dst.Kind() == reflect.Struct && !dst.Addr().Type().Implements(jsonUnmarshaler) {
		return decodeObject(raw, dst)
	}
	if dst.Kind() == reflect.Slice && dst.Type().Elem().Kind() == reflect.Struct {
		var entries []json.RawMessage
		if err := json.Unmarshal(raw, &entries); err != nil {
			return err
		}
		result := reflect.MakeSlice(dst.Type(), len(entries), len(entries))
		for i, entry := range entries {
			if err := decodeValue(entry, result.Index(i)); err != nil {
				return err
			}
		}
		dst.Set(result)
		return nil
	}
	return json.Unmarshal(raw, dst.Addr().Interface())
}

func decodeObject(raw []byte, dst reflect.Value) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return ErrMalformed
	}
	members := make([]json.RawMessage, dst.NumField())
	var ignored json.RawMessage
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return err
		}
		name, ok := token.(string)
		if !ok {
			return ErrMalformed
		}
		value := &ignored
		for i := range members {
			if strings.EqualFold(name, dst.Type().Field(i).Tag.Get("json")) {
				value = &members[i]
				break
			}
		}
		if err := d.Decode(value); err != nil {
			return err
		}
	}
	if _, err := d.Token(); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrMalformed
	}
	for i, value := range members {
		if value != nil {
			if err := decodeValue(value, dst.Field(i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// Bounded returns the canonical lists without modifying the receiver.
func (r Report) Bounded() Report {
	r.FailedUnits = smallest(r.FailedUnits, MaxFailedUnits, func(unit string) string { return unit })
	r.Slices = smallest(r.Slices, MaxSlices, func(slice Slice) string { return slice.Account })
	r.SignIns = smallest(r.SignIns, MaxSignIns, func(signIn SignIn) string { return signIn.User })
	if len(r.Error) > MaxErrorBytes {
		end := MaxErrorBytes
		for start := end - 1; start >= end-utf8.UTFMax+1; start-- {
			if _, width := utf8.DecodeRuneInString(r.Error[start:]); start+width > end {
				end = start
				break
			}
		}
		r.Error = r.Error[:end]
	}
	return r
}

// smallest retains only the first limit entries of a stable sort by key.
func smallest[T any](values []T, limit int, key func(T) string) []T {
	if values == nil {
		return nil
	}
	result := make([]T, 0, min(limit, len(values)))
	for _, value := range values {
		valueKey := key(value)
		var i int
		// Insert after equal keys to preserve their input order.
		for lo, hi := 0, len(result); lo < hi; {
			mid := lo + (hi-lo)/2
			if key(result[mid]) <= valueKey {
				lo = mid + 1
			} else {
				hi = mid
			}
			i = lo
		}
		if i == limit {
			continue
		}
		if len(result) == limit {
			result = result[:limit-1]
		}
		result = slices.Insert(result, i, value)
	}
	return result
}

// Digest streams the input into a SHA-256 digest.
func Digest(r io.Reader) (string, error) {
	hash := sha256.New()
	// Hide WriterTo so even readers that implement it use the bounded buffer.
	input := struct{ io.Reader }{r}
	if _, err := io.CopyBuffer(hash, input, make([]byte, DigestBuffer)); err != nil {
		return "", err
	}
	return DigestPrefix + hex.EncodeToString(hash.Sum(nil)), nil
}
