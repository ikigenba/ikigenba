package pages

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/store"
)

func stamps(t time.Time, layout string) (string, string) {
	utc := t.UTC()
	return utc.Format(time.RFC3339), utc.Format(layout)
}

func optionalStamps(t time.Time, layout string) (string, string) {
	if t.IsZero() {
		return "", ""
	}
	return stamps(t, layout)
}

func hostRow(h store.Host, domain string, now time.Time, flagged int) HostRow {
	reported, reportedText := optionalStamps(h.Reported, MinuteLayout)
	closed, closedText := optionalStamps(h.Closed, MinuteLayout)
	dnsName := ""
	if domain != "" {
		dnsName = h.Name + "." + domain
	}
	return HostRow{
		Name: h.Name, DNSName: dnsName, State: h.State, Reason: cmp.Or(h.Reason, h.RecordReason), OnDelete: h.OnDelete,
		Health: store.Health(h, now), Type: h.Type,
		RootGB: h.RootGB, DataGB: h.DataGB, EIP: h.EIP, AccountCount: h.Accounts,
		Reported: reported, ReportedText: reportedText, Closed: closed, ClosedText: closedText, BackupFlagged: flagged,
	}
}

func keptVolumeRow(v store.KeptVolume) KeptVolumeRow {
	deleted, deletedText := stamps(v.Deleted, MinuteLayout)
	return KeptVolumeRow{Volume: v.Volume, Host: v.Host, Cause: v.Cause, SizeGB: v.SizeGB, Deleted: deleted, DeletedText: deletedText}
}

func accountRow(a store.Account, domain string, facts store.AccountBackup, settings store.BackupSettings, now time.Time) AccountRow {
	created, createdText := stamps(a.Created, MinuteLayout)
	snapshot, snapshotText := optionalStamps(facts.Snapshot, MinuteLayout)
	return AccountRow{
		Slug: a.Slug, Host: a.Host, DNSName: a.Slug + "." + domain, State: a.State, Reason: a.Reason,
		UserCount: a.Users, InviteCount: a.Invites, Created: created, CreatedText: createdText,
		Backup: store.BackupHealth(facts, settings, now), Snapshot: snapshot, SnapshotText: snapshotText,
	}
}

func userRow(u store.User) UserRow {
	signIn, signInText := optionalStamps(u.LastSignIn, MinuteLayout)
	return UserRow{
		Login: u.Name, Account: u.Account, Admin: u.Admin, State: u.State, Pending: u.Pending,
		KeyCount: len(u.Keys), LastSignIn: signIn, LastSignInText: signInText,
	}
}

func hostView(h store.Host, domain string, now time.Time, flagged int) HostView {
	row := hostRow(h, domain, now, flagged)
	created, createdText := stamps(h.Created, SecondLayout)
	return HostView{
		Name: row.Name, DNSName: row.DNSName, State: row.State, Reason: row.Reason, OnDelete: row.OnDelete, Health: row.Health, Type: row.Type,
		RootGB: row.RootGB, DataGB: row.DataGB, EIP: row.EIP, AccountCount: row.AccountCount,
		Reported: row.Reported, ReportedText: row.ReportedText, Closed: row.Closed, ClosedText: row.ClosedText, BackupFlagged: row.BackupFlagged,
		Replace: h.Replace, InstanceID: h.InstanceID, DataVolume: h.DataVolume,
		Created: created, CreatedText: createdText, Allowlist: append([]string{}, h.Allowlist...),
	}
}

func reportView(report *store.Report, wanted int64) *ReportView {
	if report == nil {
		return nil
	}
	received, receivedText := stamps(report.Received, SecondLayout)
	body := report.Body
	booted, bootedText := optionalStamps(body.Booted, SecondLayout)
	rows := make([]SliceRow, 0, len(body.Slices))
	for _, s := range body.Slices {
		rows = append(rows, SliceRow{Account: s.Account, CPUPercent: s.CPUPercent, MemoryBytes: s.MemoryBytes})
	}
	slices.SortFunc(rows, func(a, b SliceRow) int { return strings.Compare(a.Account, b.Account) })
	bans := make([]BanRow, 0, len(body.Bans))
	for _, ban := range body.Bans {
		banned, bannedText := stamps(ban.Banned, SecondLayout)
		expires, expiresText := stamps(ban.Expires, SecondLayout)
		bans = append(bans, BanRow{Address: ban.Address, Banned: banned, BannedText: bannedText, Expires: expires, ExpiresText: expiresText})
	}
	return &ReportView{
		Received: received, ReceivedText: receivedText, Hostname: body.Hostname, InstanceID: body.InstanceID, Type: body.Type, AMI: body.AMI,
		OS: body.OS, Kernel: body.Kernel, DaemonVersion: body.DaemonVersion, Booted: booted, BootedText: bootedText,
		Applied: body.Applied, Wanted: wanted, Error: body.Error, Load1: body.Load[0], Load5: body.Load[1], Load15: body.Load[2],
		CPUPercent: body.CPUPercent, MemoryUsed: body.MemoryUsed, MemoryAvailable: body.MemoryAvailable,
		SwapUsed: body.SwapUsed, SwapTotal: body.SwapTotal, Root: VolumeView(body.Root), Data: VolumeView(body.Data),
		Sessions: body.Sessions, Updates: body.Updates, Reboot: body.Reboot,
		FailedUnits: append([]string{}, body.FailedUnits...), Slices: rows,
		Bans: bans, BanTotal: body.BanTotal, Allowlist: append([]string{}, body.Allowlist...),
	}
}
