// Package valid defines the inputs accounts accepts and its per-host limits.
package valid

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Validation errors identify which rule an input failed.
var (
	ErrName     = errors.New("invalid login name")
	ErrSlug     = errors.New("invalid account slug")
	ErrReserved = errors.New("reserved name")
	ErrKey      = errors.New("invalid SSH public key")
	ErrType     = errors.New("invalid instance type")
	ErrSize     = errors.New("invalid volume size")
)

// Instance defaults, size ranges and per-host capacity.
const (
	DefaultType        = "t4g.medium"
	DefaultRootGB      = 20
	DefaultDataGB      = 50
	MinRootGB          = 20
	MaxRootGB          = 100
	MinDataGB          = 10
	MaxDataGB          = 1024
	MaxAccountsPerHost = 50
	MaxUsersPerHost    = 200
	MaxKeysPerUser     = 10
	MaxKeyBytes        = 512
)

var (
	loginPattern  = regexp.MustCompile("^[a-z_][a-z0-9_-]*$")
	slugPattern   = regexp.MustCompile("^[a-z]([a-z0-9-]*[a-z0-9])?$")
	hostPattern   = regexp.MustCompile("^host[0-9]+$")
	labels        = strings.Fields("www mail smtp imap pop pop3 ftp api app admin root ns ns1 ns2 dns mx autodiscover autoconfig webmail status support help docs blog cdn static assets auth login sso vpn git ssh dev test staging prod localhost host hosts dnd eastmark")
	system        = strings.Fields("root bin daemon adm lp sync shutdown halt mail operator games ftp nobody systemd-network systemd-resolve systemd-timesync systemd-coredump systemd-oom dbus polkitd sshd chrony rpc rpcuser tss ec2-user ec2-instance-connect ssm-user nginx ikigenba shell wheel users acct accountsd data home srv nfsnobody libstoragemgmt sys tty disk mem kmem cdrom man dialout floppy tape video lock audio utmp input kvm render sgx systemd-journal systemd-journal-remote ssh_keys utempter screen tcpdump stapusr stapsys stapdev uuidd sssd")
	instanceTypes = strings.Fields("t4g.small t4g.medium t4g.large t4g.xlarge t4g.2xlarge m7g.medium m7g.large m7g.xlarge m7g.2xlarge m7g.4xlarge m7g.8xlarge m7g.12xlarge m7g.16xlarge c7g.medium c7g.large c7g.xlarge c7g.2xlarge c7g.4xlarge c7g.8xlarge c7g.12xlarge c7g.16xlarge r7g.medium r7g.large r7g.xlarge r7g.2xlarge r7g.4xlarge r7g.8xlarge r7g.12xlarge r7g.16xlarge")
)

// ReservedLabels returns a copy of the reserved DNS labels.
func ReservedLabels() []string { return slices.Clone(labels) }

// SystemNames returns a copy of the reserved system names.
func SystemNames() []string { return slices.Clone(system) }

// Reserved reports whether a spelling is reserved.
func Reserved(s string) bool {
	return strings.HasPrefix(s, "_") || hostPattern.MatchString(s) || slices.Contains(labels, s) || slices.Contains(system, s)
}

// Name checks a Linux login name.
func Name(s string) error {
	if len(s) > 32 || !loginPattern.MatchString(s) {
		return ErrName
	}
	if Reserved(s) {
		return ErrReserved
	}
	return nil
}

// Slug checks an account's Linux name and DNS label.
func Slug(s string) error {
	if len(s) > 32 || !slugPattern.MatchString(s) {
		return ErrSlug
	}
	if Reserved(s) {
		return ErrReserved
	}
	return nil
}

// Key holds an SSH public key and its optional comment.
type Key struct{ Type, Body, Comment string }

const keyType = "ssh-ed25519"

// ParseKey checks one OpenSSH public key line.
func ParseKey(line string) (Key, error) {
	if len(line) > MaxKeyBytes || !utf8.ValidString(line) {
		return Key{}, ErrKey
	}
	for i := range len(line) {
		b := line[i]
		if (b < 0x20 && b != '\t') || b == 0x7f {
			return Key{}, ErrKey
		}
	}
	line = strings.Trim(line, " \t")
	start := strings.IndexAny(line, " \t")
	if start < 0 || line[:start] != keyType {
		return Key{}, ErrKey
	}
	body := strings.TrimLeft(line[start:], " \t")
	comment := ""
	if end := strings.IndexAny(body, " \t"); end >= 0 {
		body, comment = body[:end], strings.TrimLeft(body[end:], " \t")
	}
	var wire [51]byte
	if len(body) != base64.StdEncoding.EncodedLen(len(wire)) {
		return Key{}, ErrKey
	}
	count, err := base64.StdEncoding.Decode(wire[:], []byte(body))
	if err != nil || count != len(wire) || binary.BigEndian.Uint32(wire[:4]) != 11 ||
		string(wire[4:15]) != keyType || binary.BigEndian.Uint32(wire[15:19]) != 32 {
		return Key{}, ErrKey
	}
	return Key{keyType, body, comment}, nil
}

// Line returns the canonical authorized_keys entry.
func (k Key) Line() string {
	line := k.Type + " " + k.Body
	if comment := strings.TrimRight(k.Comment, " \t"); comment != "" {
		line += " " + comment
	}
	return line
}

// AllowedTypes returns a copy of the allowed EC2 types.
func AllowedTypes() []string { return slices.Clone(instanceTypes) }

// Type checks an EC2 instance type.
func Type(t string) error {
	if !slices.Contains(instanceTypes, t) {
		return ErrType
	}
	return nil
}

// RootGB checks the requested root volume size.
func RootGB(gb int) error {
	if gb < MinRootGB || gb > MaxRootGB {
		return ErrSize
	}
	return nil
}

// DataGB checks the requested data volume size.
func DataGB(gb int) error {
	if gb < MinDataGB || gb > MaxDataGB {
		return ErrSize
	}
	return nil
}
