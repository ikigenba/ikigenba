package tools_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
)

// R-52LG-7L7P R-64EC-2H78
func TestFirstLine(t *testing.T) {
	type firstLineFunc func(string) string
	var firstLine firstLineFunc = tools.FirstLine
	for _, tc := range []struct{ in, want string }{
		{"first\nsecond", "first"}, {"", ""}, {"all", "all"}, {"\nremaining", ""},
	} {
		if firstLine(tc.in) != tc.want {
			t.Fatal(tc)
		}
	}
}

// R-Y3NM-ABQI R-YH2I-HSW5 R-ZB0K-I008 R-ZC8G-VRQX R-CSDA-A2TO R-CPXH-IJCA R-0VXT-EZQU R-YFUM-415G R-0UPX-1805 R-HIWJ-5E6D R-HLCB-WXNR R-HHOM-RMFO R-4JHB-J3J5
// R-VVBY-ASMX R-VWJU-OKDM
func TestCopyAndTimeout(t *testing.T) {
	const (
		_ = tools.NotConfigured
		_ = tools.InvalidName
		_ = tools.ReservedName
		_ = tools.NameTaken
		_ = tools.NoHost
		_ = tools.NoAccount
		_ = tools.NoUser
		_ = tools.NotInAccount
		_ = tools.InvalidKey
		_ = tools.KeyNotHeld
		_ = tools.TypeNotAllowed
		_ = tools.SizeOutOfRange
		_ = tools.SizeShrinks
		_ = tools.NothingToChange
		_ = tools.HostBusy
		_ = tools.HostFailed
		_ = tools.HostNotFailed
		_ = tools.HostNotRunning
		_ = tools.HostNotStopped
		_ = tools.HostHasAccounts
		_ = tools.HostFull
		_ = tools.HostGone
		_ = tools.AccountHasUsers
		_ = tools.AccountRemoving
		_ = tools.AccountFull
		_ = tools.TooManyKeys
		_ = tools.GrowLimit
		_ = tools.UserNotLocked
		_ = tools.UserRemoving
		_ = tools.NameHasRecord
		_ = tools.CloudFailed
		_ = tools.InvalidAddress
		_ = tools.PrefixTooWide
		_ = tools.TooManyUnbans
		_ = tools.AllowlistFull
		_ = tools.NoLink
		_ = tools.NoInvite
		_ = tools.TooManyInvites
		_ = tools.NoKeptVolume
		_ = tools.VolumeDeleting
		_ = tools.HostNotClosed
		_ = tools.HostWithheld
		_ = tools.NoDataVolume
		_ = tools.KeepsVolumeSize
		_ = tools.BadReplace
		_ = tools.InvalidPath
		_ = tools.BadTime
		_ = tools.AccountNotActive
		_ = tools.NoDatabase
		_ = tools.NotRestorable
		_ = tools.NoSnapshot
		_ = tools.TooManyRestores
		_ = tools.AccountExists
		_ = tools.NameHasBackups
		_ = tools.BackupsDeleting
		_ = tools.NoAddress
		_ = tools.ArgAdmin
		_ = tools.ArgInvite
		_ = tools.ArgIP
		_ = tools.ArgHost
		_ = tools.ArgAccount
		_ = tools.ArgUser
		_ = tools.ArgKey
		_ = tools.ArgType
		_ = tools.ArgRootGB
		_ = tools.ArgDataGB
		_ = tools.ArgKeepData
		_ = tools.ArgVolume
		_ = tools.ArgURL
		_ = tools.ArgRef
		_ = tools.InvalidURL
		_ = tools.InvalidRef
	)
	refusals := []string{
		tools.NotConfigured, tools.InvalidName, tools.ReservedName, tools.NameTaken,
		tools.NoHost, tools.NoAccount, tools.NoUser, tools.NotInAccount, tools.InvalidKey,
		tools.KeyNotHeld, tools.TypeNotAllowed, tools.SizeOutOfRange, tools.SizeShrinks,
		tools.NothingToChange, tools.HostBusy, tools.HostFailed, tools.HostNotFailed,
		tools.HostNotRunning, tools.HostNotStopped, tools.HostHasAccounts, tools.HostFull,
		tools.HostGone, tools.AccountHasUsers, tools.AccountRemoving, tools.AccountFull,
		tools.TooManyKeys, tools.GrowLimit, tools.UserNotLocked, tools.UserRemoving,
		tools.NameHasRecord, tools.CloudFailed, tools.InvalidAddress, tools.PrefixTooWide, tools.TooManyUnbans, tools.AllowlistFull, tools.NoLink, tools.NoInvite, tools.TooManyInvites, tools.NoKeptVolume, tools.VolumeDeleting,
		tools.HostNotClosed, tools.HostWithheld, tools.NoDataVolume, tools.KeepsVolumeSize, tools.BadReplace, tools.InvalidPath,
		tools.BadTime, tools.AccountNotActive, tools.NoDatabase, tools.NotRestorable, tools.NoSnapshot,
		tools.TooManyRestores, tools.AccountExists, tools.NameHasBackups, tools.BackupsDeleting, tools.NoAddress,
		tools.InvalidURL, tools.InvalidRef,
	}
	seen := map[string]bool{store.Unreachable: true}
	for _, copy := range refusals {
		if copy == "" || strings.Contains(copy, "\n") || seen[copy] {
			t.Fatalf("invalid refusal constant %q", copy)
		}
		seen[copy] = true
	}
	for _, copy := range []string{
		tools.ArgHost, tools.ArgAccount, tools.ArgUser, tools.ArgKey,
		tools.ArgType, tools.ArgRootGB, tools.ArgDataGB, tools.ArgIP,
		tools.ArgAdmin, tools.ArgInvite, tools.ArgKeepData, tools.ArgVolume,
		tools.ArgFrom, tools.ArgLost, tools.ArgPath, tools.ArgAsOf, tools.ArgSnapshot,
		tools.ArgURL, tools.ArgRef,
	} {
		if copy == "" || strings.Contains(copy, "\n") {
			t.Fatalf("invalid argument description %q", copy)
		}
	}
	const timeout = tools.DNSCheckTimeout
	const backupTimeout = tools.BackupCheckTimeout
	if backupTimeout != 5*time.Second {
		t.Fatal(backupTimeout)
	}
	if timeout != 5*time.Second {
		t.Fatal(timeout)
	}
}
