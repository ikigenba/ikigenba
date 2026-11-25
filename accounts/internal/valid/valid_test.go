package valid_test

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

// R-J4IF-828R R-J5QB-LTZG R-JGPF-1RNP R-JHXB-FJEE R-JJ57-TB53
func TestNames(t *testing.T) {
	sentinels := []error{valid.ErrName, valid.ErrSlug, valid.ErrReserved, valid.ErrKey, valid.ErrType, valid.ErrSize}
	for i, err := range sentinels {
		if err == nil {
			t.Fatal("nil sentinel", i)
		}
		for j, other := range sentinels {
			if i != j && errors.Is(err, other) {
				t.Fatal("overlapping sentinels", i, j)
			}
		}
	}
	for _, tc := range []struct {
		name                string
		reserved            bool
		userErr, accountErr error
	}{
		{"", false, valid.ErrName, valid.ErrSlug},
		{"daffy", false, nil, nil},
		{"d_uck-2", false, nil, valid.ErrSlug},
		{"a", false, nil, nil},
		{"acme-2", false, nil, nil},
		{"acme-", false, nil, valid.ErrSlug},
		{"2daffy", false, valid.ErrName, valid.ErrSlug},
		{"Daffy", false, valid.ErrName, valid.ErrSlug},
		{"daf fy", false, valid.ErrName, valid.ErrSlug},
		{"daffy.d", false, valid.ErrName, valid.ErrSlug},
		{"-daffy", false, valid.ErrName, valid.ErrSlug},
		{"_svc", true, valid.ErrReserved, valid.ErrSlug},
		{"host01", true, valid.ErrReserved, valid.ErrReserved},
		{"host7", true, valid.ErrReserved, valid.ErrReserved},
		{"host100", true, valid.ErrReserved, valid.ErrReserved},
		{"hostel", false, nil, nil},
		{"host-01", false, nil, nil},
		{"myhost01", false, nil, nil},
		{"hosts1", false, nil, nil},
		{"WWW", false, valid.ErrName, valid.ErrSlug},
		{strings.Repeat("a", 32), false, nil, nil},
		{strings.Repeat("a", 33), false, valid.ErrName, valid.ErrSlug},
	} {
		if valid.Reserved(tc.name) != tc.reserved || !errors.Is(valid.Name(tc.name), tc.userErr) || !errors.Is(valid.Slug(tc.name), tc.accountErr) {
			t.Fatalf("validation for %q", tc.name)
		}
	}
}

// R-J6Y7-ZLQ5 R-JD1P-WGFM R-0VDM-AO4K R-JMSW-YMD6
func TestListsAreExactAndIndependent(t *testing.T) {
	for _, tc := range []struct {
		get   func() []string
		want  string
		check func(string) bool
	}{
		{valid.ReservedLabels, "www mail smtp imap pop pop3 ftp api app admin root ns ns1 ns2 dns mx autodiscover autoconfig webmail status support help docs blog cdn static assets auth login sso vpn git ssh dev test staging prod localhost host hosts dnd eastmark", valid.Reserved},
		{valid.SystemNames, "root bin daemon adm lp sync shutdown halt mail operator games ftp nobody systemd-network systemd-resolve systemd-timesync systemd-coredump systemd-oom dbus polkitd sshd chrony rpc rpcuser tss ec2-user ec2-instance-connect ssm-user nginx ikigenba shell wheel users acct accountsd data home srv nfsnobody libstoragemgmt sys tty disk mem kmem cdrom man dialout floppy tape video lock audio utmp input kvm render sgx systemd-journal systemd-journal-remote ssh_keys utempter screen tcpdump stapusr stapsys stapdev stapunpriv uuidd sssd", valid.Reserved},
		{valid.AllowedTypes, "t4g.small t4g.medium t4g.large t4g.xlarge t4g.2xlarge m7g.medium m7g.large m7g.xlarge m7g.2xlarge m7g.4xlarge m7g.8xlarge m7g.12xlarge m7g.16xlarge c7g.medium c7g.large c7g.xlarge c7g.2xlarge c7g.4xlarge c7g.8xlarge c7g.12xlarge c7g.16xlarge r7g.medium r7g.large r7g.xlarge r7g.2xlarge r7g.4xlarge r7g.8xlarge r7g.12xlarge r7g.16xlarge", func(s string) bool { return valid.Type(s) == nil }},
	} {
		a, b := tc.get(), tc.get()
		want := strings.Fields(tc.want)
		if !slices.Equal(a, want) {
			t.Fatal(a, want)
		}
		clear(a)
		if !slices.Equal(b, want) || !slices.Equal(tc.get(), want) {
			t.Fatal("returned list shares storage")
		}
		for _, s := range b {
			if !tc.check(s) {
				t.Fatal("list entry ignored", s)
			}
		}
	}
}

// R-J864-DDGU R-J3AI-UAI2 R-OLQD-HRVF
func TestSSHKey(t *testing.T) {
	const maxKeyBytes int16 = valid.MaxKeyBytes
	if maxKeyBytes != 512 {
		t.Fatal(maxKeyBytes)
	}
	raw := make([]byte, 51)
	binary.BigEndian.PutUint32(raw[:4], 11)
	copy(raw[4:], "ssh-ed25519")
	binary.BigEndian.PutUint32(raw[15:19], 32)
	copy(raw[19:], "0123456789abcdefghijklmnopqrstuv")
	body := base64.StdEncoding.EncodeToString(raw)
	for _, comment := range []string{"", "daffy@example.com", "two words", "two\twords", strings.Repeat("x", int(maxKeyBytes)-len(body)-15)} {
		line := "ssh-ed25519 " + body
		want := line
		if comment != "" {
			line += "\t  " + comment
			want += " " + comment
		}
		key, err := valid.ParseKey(line)
		if err != nil || key != (valid.Key{Type: "ssh-ed25519", Body: body, Comment: comment}) || key.Line() != want {
			t.Fatal(key, err, want)
		}
		again, err := valid.ParseKey(key.Line())
		if err != nil || again != key || again.Line() != want {
			t.Fatal("canonical form", again, err)
		}
	}
	key, err := valid.ParseKey("  ssh-ed25519\t" + body + "  ")
	if err != nil || key.Comment != "" || key.Body != body {
		t.Fatal(key, err)
	}
}

// R-K74K-TPPE
func TestRejectedSSHKeys(t *testing.T) {
	var raw [51]byte
	binary.BigEndian.PutUint32(raw[:4], 11)
	copy(raw[4:], "ssh-ed25519")
	binary.BigEndian.PutUint32(raw[15:19], 32)
	copy(raw[48:], []byte{0xfb, 0xff, 0xff})
	body := base64.StdEncoding.EncodeToString(raw[:])
	wrong := raw
	wrong[14] = '8'
	wrongType := base64.StdEncoding.EncodeToString(wrong[:])
	for _, input := range []string{
		"", "   ", "ssh-ed25519", "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAAAAwECAw==", "sk-ssh-ed25519@openssh.com " + body,
		"no-pty ssh-ed25519 " + body, "ssh-ed25519 " + body[:len(body)-1],
		"ssh-ed25519 " + strings.Replace(body, "+", "-", 1), "ssh-ed25519 " + strings.Replace(body, "/", "_", 1),
		"ssh-ed25519 " + wrongType, "ssh-ed25519 " + base64.StdEncoding.EncodeToString(raw[:50]),
		"ssh-ed25519 " + base64.StdEncoding.EncodeToString(append(raw[:], 2)), "ssh-ed25519 " + body + " \xff",
		"ssh-ed25519 " + body + " \rcomment", "ssh-ed25519 " + body + "\nssh-ed25519 " + body,
		"ssh-ed25519 " + body + " " + strings.Repeat("x", valid.MaxKeyBytes-len(body)-12),
	} {
		key, err := valid.ParseKey(input)
		if key != (valid.Key{}) || !errors.Is(err, valid.ErrKey) {
			t.Fatalf("accepted %q: %#v %v", input, key, err)
		}
	}
}

// R-J9E0-R57J R-JALX-4WY8 R-JBTT-IOOX R-JO0T-CE3V R-JP8P-Q5UK
func TestHostSizes(t *testing.T) {
	const _ int16 = valid.DefaultRootGB + valid.DefaultDataGB + valid.MinRootGB + valid.MaxRootGB + valid.MinDataGB + valid.MaxDataGB
	const _ uint32 = valid.MaxAccountsPerHost + valid.MaxUsersPerHost + valid.MaxKeysPerUser
	if valid.MaxAccountsPerHost != 50 || valid.MaxUsersPerHost != 200 || valid.MaxKeysPerUser != 10 ||
		valid.DefaultType != "t4g.medium" || valid.DefaultRootGB != 20 || valid.DefaultDataGB != 50 ||
		valid.MinRootGB != 20 || valid.MaxRootGB != 100 || valid.MinDataGB != 10 || valid.MaxDataGB != 1024 {
		t.Fatal("host defaults or caps")
	}
	for _, s := range []string{"", "t4g.nano", "t4g.micro", "m7g.metal", "t3.medium", "T4G.MEDIUM", "m6g.large"} {
		if !errors.Is(valid.Type(s), valid.ErrType) {
			t.Fatal("accepted type", s)
		}
	}
	for size := -20; size <= 1025; size++ {
		wantRoot, wantData := valid.ErrSize, valid.ErrSize
		if size >= 20 && size <= 100 {
			wantRoot = nil
		}
		if size >= 10 && size <= 1024 {
			wantData = nil
		}
		if !errors.Is(valid.RootGB(size), wantRoot) || !errors.Is(valid.DataGB(size), wantData) {
			t.Fatal("size", size)
		}
	}
}

// R-IFJS-83M2
func TestMaximumCallerIDBytes(t *testing.T) {
	type size int
	const limit size = valid.MaxUserIDBytes
	if limit != 64 {
		t.Fatalf("MaxUserIDBytes = %d, want 64", limit)
	}
}
