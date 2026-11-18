package linux_test

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/linux"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

const banQuery = "fail2ban-client get sshd banip --with-time"
const unbanPrefix = "fail2ban-client set sshd unbanip "

func writeBanOutput(t *testing.T, c linux.Command, chunks ...string) {
	t.Helper()
	if c.Stdout == nil {
		t.Fatal("ban query does not stream stdout")
	}
	for _, chunk := range chunks {
		if n, err := io.WriteString(c.Stdout, chunk); err != nil || n != len(chunk) {
			t.Fatalf("writing ban output = %d, %v", n, err)
		}
	}
}

func TestApplyBans(t *testing.T) {
	// R-ZHGD-K9Z9 R-ZMBZ-3CY1 R-RRG9-Z97I
	state := protocol.DesiredState{
		Allowlist: []string{"192.0.2.0/24", "2001:db8::1"},
		Unban:     []string{"198.51.100.77", "198.51.100.7"},
	}
	t.Run("running jail", func(t *testing.T) {
		f := newHardeningFixture(t)
		f.observe = func(c linux.Command) {
			if c.Name == "fail2ban-client" && c.Args[0] == "get" {
				writeBanOutput(t, c,
					"198.51.100.7 \tuncovered\n192.0.2.77\t",
					"covered\n2001:DB8::1 noncanonical\n",
					"192.0.2.100 "+strings.Repeat("x", linux.MaxBanLine)+"\n",
					"192.0.2.7 "+strings.Repeat("x", linux.MaxBanLine-len("192.0.2.7 "))+"\n",
					"2001:db8::1",
				)
			}
		}
		f.apply(state)
		f.expectFile(hardenJail, strings.TrimSuffix(wantJail, "\n")+" 192.0.2.0/24 2001:db8::1\n", 0o644)
		want := []string{unbanPrefix + state.Unban[0], unbanPrefix + state.Unban[1], banQuery,
			unbanPrefix + "192.0.2.77", unbanPrefix + "192.0.2.7", unbanPrefix + "2001:db8::1"}
		if got := banCommands(f.commands); !slices.Equal(got, want) {
			t.Fatalf("ban commands = %v, want %v", got, want)
		}
		f.commands = nil
		f.apply(protocol.DesiredState{})
		f.forbidCalls("fail2ban-client ")
		f.commands = nil
		f.apply(protocol.DesiredState{Unban: state.Unban})
		if got := banCommands(f.commands); !slices.Equal(got, want[:len(state.Unban)]) {
			t.Fatalf("ban commands without an allowlist = %v", got)
		}
	})
	for _, query := range []string{"is-enabled", "is-active"} {
		t.Run(query, func(t *testing.T) {
			f := newHardeningFixture(t)
			f.answer = func(call string) (linux.Result, bool) {
				return linux.Result{ExitCode: 3}, call == "systemctl "+query+" fail2ban.service"
			}
			f.apply(state)
			start := slices.Index(f.commands, "systemctl enable --now fail2ban.service")
			unban := slices.Index(f.commands, unbanPrefix+state.Unban[0])
			if start < 0 || unban <= start {
				t.Fatalf("jail start and unban commands = %v", f.commands)
			}
			if got := banCommands(f.commands); !slices.Equal(got, []string{unbanPrefix + state.Unban[0], unbanPrefix + state.Unban[1]}) {
				t.Fatalf("ban commands after jail start = %v", got)
			}
			if query == "is-active" {
				f.forbidCalls("systemctl reload fail2ban.service")
			}
		})
	}
	t.Run("lift cap", func(t *testing.T) {
		f := newHardeningFixture(t)
		want := []string{banQuery}
		f.observe = func(c linux.Command) {
			if c.Name != "fail2ban-client" || c.Args[0] != "get" {
				return
			}
			for i := 0; i < protocol.MaxBans+1; i++ {
				address := fmt.Sprintf("2001:db8::%x", i+1)
				writeBanOutput(t, c, address+"\n")
				if i < protocol.MaxBans {
					want = append(want, unbanPrefix+address)
				}
			}
		}
		f.apply(protocol.DesiredState{Allowlist: []string{"2001:db8::/48"}})
		if got := banCommands(f.commands); !slices.Equal(got, want) {
			t.Fatalf("allowed lifts = %d, want %d in stream order", len(got)-1, len(want)-1)
		}
	})
	for _, failed := range []string{unbanPrefix + state.Unban[0], banQuery, unbanPrefix + "2001:db8::1"} {
		t.Run(failed, func(t *testing.T) {
			f := newHardeningFixture(t)
			f.observe = func(c linux.Command) {
				if c.Name == "fail2ban-client" && c.Args[0] == "get" {
					writeBanOutput(t, c, "2001:db8::1\n")
				}
			}
			f.answer = func(call string) (linux.Result, bool) {
				return linux.Result{ExitCode: 7}, call == failed
			}
			f.applyFails(state, failed, "fail2ban-client")
		})
	}
}

func banCommands(commands []string) []string {
	var calls []string
	for _, call := range commands {
		if strings.HasPrefix(call, "fail2ban-client ") {
			calls = append(calls, call)
		}
	}
	return calls
}

func TestInspectBans(t *testing.T) {
	// R-ZORR-UWFF R-ZPZO-8O64
	const _ int16 = linux.MaxBanLine
	const _ uint16 = linux.MaxBanLine
	if linux.MaxBanLine != 512 {
		t.Fatalf("MaxBanLine = %d, want 512", linux.MaxBanLine)
	}
	const timeLayout = "2006-01-02 15:04:05"
	for _, zone := range []*time.Location{time.UTC, time.FixedZone("test zone", -5*3600)} {
		t.Run(zone.String(), func(t *testing.T) {
			start := time.Date(2026, 11, 18, 14, 2, 11, 0, zone)
			end := start.Add(601 * time.Second)
			output := func(address string, start, end time.Time) string {
				return address + " \t" + start.Format(timeLayout) + " + 600 = " + end.Format(timeLayout)
			}
			host := linux.New(linux.Env{Root: t.TempDir(), Now: func() time.Time { return start },
				Execute: func(_ context.Context, c linux.Command) (linux.Result, error) {
					if c.Name != "fail2ban-client" {
						return linux.Result{}, nil
					}
					if !slices.Equal(c.Args, []string{"get", "sshd", "banip", "--with-time"}) {
						t.Fatalf("ban query args = %v", c.Args)
					}
					for i := range protocol.MaxBans {
						at := start.Add(-time.Duration(protocol.MaxBans-i) * time.Second)
						writeBanOutput(t, c, output("198.51.100.7", at, end)+"\n")
					}
					validLine := output("::ffff:192.0.2.77", start, end)
					exactLimit := strings.Replace(validLine, "+ 600", "+ "+strings.Repeat("9", linux.MaxBanLine-len(validLine)+3), 1)
					writeBanOutput(t, c,
						strings.Repeat(" \t", linux.MaxBanLine)+"\r\n",
						strings.Repeat(" ", linux.MaxBanLine), "x\n",
						exactLimit+"\n",
						exactLimit+"1\n",
						output("invalid address", start, end)+"\n",
						strings.Replace(validLine, " + 600", " + -1", 1)+"\n",
						strings.Replace(validLine, "14:02:11", "14:02:11.1", 1)+"\n",
						output("192.0.2.7", start, end),
					)
					return linux.Result{}, nil
				},
			})
			got, err := host.Inspect(t.Context(), protocol.DesiredState{})
			if err != nil || got.BanTotal != protocol.MaxBans+7 || len(got.Bans) != protocol.MaxBans {
				t.Fatalf("Inspect = %d bans, %d total, %v", len(got.Bans), got.BanTotal, err)
			}
			want := []protocol.Ban{
				{Address: "192.0.2.7", Banned: start.UTC(), Expires: end.UTC()},
				{Address: "192.0.2.77", Banned: start.UTC(), Expires: end.UTC()},
			}
			for i := 1; len(want) < protocol.MaxBans; i++ {
				want = append(want, protocol.Ban{Address: "198.51.100.7", Banned: start.Add(-time.Duration(i) * time.Second).UTC(), Expires: end.UTC()})
			}
			if !slices.Equal(got.Bans, want) {
				t.Fatal("reported bans differ from the newest entries in descending start and ascending address order")
			}
		})
	}
}

func TestInspectBansUnavailable(t *testing.T) {
	// R-ZPZO-8O64
	for _, tc := range []struct {
		name, stdout string
		exit         int
		err          error
	}{
		{name: "empty", stdout: "\n"},
		{name: "failed", stdout: "192.0.2.77 \t2026-11-18 14:02:11 + 600 = 2026-11-18 14:12:12\n", exit: 1},
		{name: "missing", err: io.ErrUnexpectedEOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := linux.New(linux.Env{Root: t.TempDir(), Now: func() time.Time { return time.Unix(1000, 0).UTC() },
				Execute: func(_ context.Context, c linux.Command) (linux.Result, error) {
					if c.Name == "fail2ban-client" {
						writeBanOutput(t, c, tc.stdout)
						return linux.Result{ExitCode: tc.exit}, tc.err
					}
					return linux.Result{}, nil
				},
			})
			got, err := host.Inspect(t.Context(), protocol.DesiredState{})
			if err != nil || len(got.Bans) != 0 || got.BanTotal != 0 {
				t.Fatalf("Inspect = %+v, %v", got, err)
			}
		})
	}
}

func TestInspectAppliedAllowlist(t *testing.T) {
	// R-ZR7K-MFWT
	f := newHardeningFixture(t)
	s := protocol.DesiredState{Allowlist: []string{"198.51.100.0/24", "192.0.2.7"}}
	check := func(want []string) {
		t.Helper()
		got, err := f.host.Inspect(t.Context(), s)
		if err != nil || !slices.Equal(got.Allowlist, want) {
			t.Fatalf("Inspect allowlist = %v, %v, want %v", got.Allowlist, err, want)
		}
	}
	check(nil)
	f.apply(s)
	check([]string{"192.0.2.7", "198.51.100.0/24"})
	f.put(hardenJail, "ignoreip = 127.0.0.1/8\t::1 192.0.2.7\nignoreip = 198.51.100.0/24\n", 0o644)
	check([]string{"192.0.2.7"})
	f.put(hardenJail, strings.Repeat("x", linux.MaxCapture)+"\nignoreip = 192.0.2.7\n", 0o644)
	check(nil)
	want := make([]string, protocol.MaxAllowlist)
	contents := "ignoreip = 127.0.0.1/8 ::1 2001:db8::1"
	for i := len(want) - 1; i >= 0; i-- {
		want[i] = fmt.Sprintf("192.0.2.%d", i+1)
		contents += "\t" + want[i]
	}
	slices.Sort(want)
	f.put(hardenJail, contents, 0o644)
	check(want)
}
