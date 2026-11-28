// Package protocol defines the wire contract between accounts and accountsd.
package protocol

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// The daemon fleet uses these paths, query names, and media types.
const (
	BinaryPath      = "/accountsd"
	StatePath       = "/accountsd/v1/state"
	ReportPath      = "/accountsd/v1/report"
	BoxPath         = "/accountsd/v1/box"
	QueryAfter      = "after"
	QueryDigest     = "digest"
	QueryUID        = "uid"
	MethodToolsList = "tools/list"
	MethodToolsCall = "tools/call"
	ContentJSON     = "application/json"
	ContentBinary   = "application/octet-stream"
	DigestPrefix    = "sha256:"
)

// Wire version, payload bounds, and polling interval.
const (
	Version        = 1
	MaxStateBytes  = 2 << 20
	MaxReportBytes = 320 << 10
	MaxBinaryBytes = 64 << 20
	MaxBoxRequest  = 64 << 10
	MaxBoxAnswer   = 1 << 20
	MaxErrorBytes  = 4096
	MaxFailedUnits = 100
	MaxSlices      = 50
	MaxSignIns     = 200
	MaxBans        = 200
	MaxAllowlist   = 50
	MaxUnbans      = 50
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
	Protocol    int       `json:"protocol"`
	Host        string    `json:"host"`
	Version     int64     `json:"version"`
	Daemon      string    `json:"daemon"`
	DataVolume  string    `json:"data_volume"`
	RootGB      int       `json:"root_gb"`
	DataGB      int       `json:"data_gb"`
	Accounts    []Account `json:"accounts"`
	Users       []User    `json:"users"`
	Allowlist   []string  `json:"allowlist"`
	Unban       []string  `json:"unban"`
	Domain      string    `json:"domain"`
	Backup      *Backup   `json:"backup"`
	TemplateURL string    `json:"template_url"`
	TemplateRef string    `json:"template_ref"`
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
	Protocol        int             `json:"protocol"`
	Attempted       int64           `json:"attempted"`
	Applied         int64           `json:"applied"`
	Error           string          `json:"error"`
	InstanceID      string          `json:"instance_id"`
	Type            string          `json:"type"`
	AMI             string          `json:"ami"`
	OS              string          `json:"os"`
	Kernel          string          `json:"kernel"`
	Booted          time.Time       `json:"booted"`
	DaemonVersion   string          `json:"daemon_version"`
	Load            [3]float64      `json:"load"`
	CPUPercent      float64         `json:"cpu_percent"`
	MemoryUsed      int64           `json:"memory_used"`
	MemoryAvailable int64           `json:"memory_available"`
	SwapUsed        int64           `json:"swap_used"`
	SwapTotal       int64           `json:"swap_total"`
	Root            Disk            `json:"root"`
	Data            Disk            `json:"data"`
	Sessions        int             `json:"sessions"`
	Updates         int             `json:"updates"`
	Reboot          bool            `json:"reboot"`
	FailedUnits     []string        `json:"failed_units"`
	Slices          []Slice         `json:"slices"`
	SignIns         []SignIn        `json:"sign_ins"`
	Bans            []Ban           `json:"bans"`
	BanTotal        int             `json:"ban_total"`
	Allowlist       []string        `json:"allowlist"`
	Hostname        string          `json:"hostname"`
	Backups         []AccountBackup `json:"backups"`
	HostBackup      HostBackup      `json:"host_backup"`
	Restores        []RestoreResult `json:"restores"`
	Seeds           []Seed          `json:"seeds"`
	Homes           []Home          `json:"homes"`
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

// Ban records one address's active SSH ban.
type Ban struct {
	Address string    `json:"address"`
	Banned  time.Time `json:"banned"`
	Expires time.Time `json:"expires"`
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
	if dst.Kind() == reflect.Pointer {
		value := reflect.New(dst.Type().Elem())
		if err := decodeValue(raw, value.Elem()); err != nil {
			return err
		}
		dst.Set(value)
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
	r.FailedUnits = smallest(r.FailedUnits, MaxFailedUnits, cmp.Compare[string])
	r.Slices = smallest(r.Slices, MaxSlices, func(a, b Slice) int { return cmp.Compare(a.Account, b.Account) })
	r.SignIns = smallest(r.SignIns, MaxSignIns, func(a, b SignIn) int { return cmp.Compare(a.User, b.User) })
	r.BanTotal = max(r.BanTotal, len(r.Bans))
	r.Bans = smallest(r.Bans, MaxBans, func(a, b Ban) int {
		if order := b.Banned.Compare(a.Banned); order != 0 {
			return order
		}
		return cmp.Compare(a.Address, b.Address)
	})
	r.Allowlist = smallest(r.Allowlist, MaxAllowlist, cmp.Compare[string])
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
	r.OS = jsonStringPrefix(r.OS, 256)
	r.boundBackups()
	r.Seeds = boundTemplateEntries(r.Seeds, MaxSeeds, func(seed Seed) (int64, string) { return seed.GID, seed.Commit })
	r.Homes = boundTemplateEntries(r.Homes, MaxHomes, func(home Home) (int64, string) { return home.UID, home.Commit })
	return r
}

// smallest retains only the first limit entries of a stable sort.
func smallest[T any](values []T, limit int, compare func(T, T) int) []T {
	if values == nil {
		return nil
	}
	result := make([]T, 0, min(limit, len(values)))
	for _, value := range values {
		result = insertSmallest(result, value, limit, compare)
	}
	return result
}

func insertSmallest[T any](result []T, value T, limit int, compare func(T, T) int) []T {
	// Insert after equal keys to preserve their input order.
	i := sort.Search(len(result), func(i int) bool { return compare(result[i], value) > 0 })
	if i == limit {
		return result
	}
	if len(result) == limit {
		result = result[:limit-1]
	}
	return slices.Insert(result, i, value)
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
