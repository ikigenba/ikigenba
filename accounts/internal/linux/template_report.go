package linux

import (
	"cmp"
	"slices"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

func (h *Host) pruneTemplateCommits(s protocol.DesiredState) {
	for gid := range h.seedCommits {
		if !slices.ContainsFunc(s.Accounts, func(a protocol.Account) bool {
			return a.GID == gid && a.State == protocol.AccountActive
		}) {
			delete(h.seedCommits, gid)
		}
	}
	for uid := range h.homeCommits {
		if !slices.ContainsFunc(s.Users, func(u protocol.User) bool {
			return u.UID == uid && (u.State == protocol.UserActive || u.State == protocol.UserLocked)
		}) {
			delete(h.homeCommits, uid)
		}
	}
}

func (h *Host) templateReport(s protocol.DesiredState, report *protocol.Report) {
	for _, a := range s.Accounts {
		if commit := h.seedCommits[a.GID]; commit != "" && a.State == protocol.AccountActive {
			report.Seeds = append(report.Seeds, protocol.Seed{GID: a.GID, Commit: commit})
		}
	}
	for _, u := range s.Users {
		if commit := h.homeCommits[u.UID]; commit != "" && (u.State == protocol.UserActive || u.State == protocol.UserLocked) {
			report.Homes = append(report.Homes, protocol.Home{UID: u.UID, Commit: commit})
		}
	}
	slices.SortFunc(report.Seeds, func(a, b protocol.Seed) int { return cmp.Compare(a.GID, b.GID) })
	slices.SortFunc(report.Homes, func(a, b protocol.Home) int { return cmp.Compare(a.UID, b.UID) })
}
