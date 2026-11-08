package accounts

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// ManifestGroup records a business group's numeric identity and members.
type ManifestGroup struct {
	Slug    string   `json:"slug"`
	GID     int      `json:"gid"`
	UID     int      `json:"uid"`
	Members []string `json:"members"`
}

// ManifestUser records an SSH user's numeric identity and access.
type ManifestUser struct {
	Name   string   `json:"name"`
	UID    int      `json:"uid"`
	GID    int      `json:"gid"`
	State  string   `json:"state"`
	Groups []string `json:"groups"`
}

// Manifest is the account identity inventory carried by host backups.
type Manifest struct {
	Groups []ManifestGroup `json:"groups"`
	Users  []ManifestUser  `json:"users"`
}

// ReadManifest reads the host's business groups and SSH users without processes.
func ReadManifest(_ context.Context, env host.Env) (Manifest, error) {
	m := Manifest{Groups: []ManifestGroup{}, Users: []ManifestUser{}}
	db, err := loadAccounts(env.Root)
	if err != nil {
		return m, err
	}
	states, err := readStates(env)
	if err != nil {
		return m, err
	}
	for name, g := range db.groups {
		if !db.businessGroup(name) {
			continue
		}
		uid, err := manifestID(db.accounts[name].uid)
		if err != nil {
			return m, err
		}
		gid, err := manifestID(g.gid)
		if err != nil {
			return m, err
		}
		m.Groups = append(m.Groups, ManifestGroup{Slug: name, UID: uid, GID: gid, Members: db.members(name)})
	}
	for name, a := range db.accounts {
		if !db.user(name) {
			continue
		}
		uid, err := manifestID(a.uid)
		if err != nil {
			return m, err
		}
		gid, err := manifestID(a.gid)
		if err != nil {
			return m, err
		}
		groups := []string{}
		for slug, g := range db.groups {
			if db.businessGroup(slug) && slices.Contains(g.members, name) {
				groups = append(groups, slug)
			}
		}
		slices.Sort(groups)
		m.Users = append(m.Users, ManifestUser{Name: name, UID: uid, GID: gid, State: userState(states, name), Groups: groups})
	}
	sortManifest(&m)
	return m, validateManifest(m)
}

func manifestID(raw string) (int, error) {
	if raw == "" {
		return 0, errors.New("empty account id")
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid account id %q", raw)
		}
	}
	id, err := strconv.ParseInt(raw, 10, strconv.IntSize)
	return int(id), err
}

func sortManifest(m *Manifest) {
	slices.SortFunc(m.Groups, func(a, b ManifestGroup) int { return strings.Compare(a.Slug, b.Slug) })
	slices.SortFunc(m.Users, func(a, b ManifestUser) int { return strings.Compare(a.Name, b.Name) })
}

func validateManifestGroup(g ManifestGroup) error {
	if err := validateNames(append([]string{g.Slug}, g.Members...)...); err != nil {
		return err
	}
	if g.UID < 0 || g.GID < 0 {
		return errors.New("negative account id")
	}
	return nil
}

func validateManifestUser(u ManifestUser) error {
	if err := validateNames(append([]string{u.Name}, u.Groups...)...); err != nil {
		return err
	}
	if u.UID < 0 || u.GID < 0 {
		return errors.New("negative account id")
	}
	if u.State != StateActive && u.State != StateLocked {
		return fmt.Errorf("invalid user state %q", u.State)
	}
	seen := map[string]bool{}
	for _, g := range u.Groups {
		if seen[g] {
			return fmt.Errorf("duplicate user group %q", g)
		}
		seen[g] = true
	}
	return nil
}

func validateManifest(m Manifest) error {
	names, uids, gids := map[string]bool{}, map[int]bool{}, map[int]bool{}
	add := func(name string, uid, gid int) error {
		if names[name] || uids[uid] || gids[gid] {
			return errors.New("duplicate manifest name or id")
		}
		names[name], uids[uid], gids[gid] = true, true, true
		return nil
	}
	groups := map[string]bool{}
	for _, g := range m.Groups {
		if err := validateManifestGroup(g); err != nil {
			return err
		}
		if err := add(g.Slug, g.UID, g.GID); err != nil {
			return err
		}
		groups[g.Slug] = true
	}
	for _, u := range m.Users {
		if err := validateManifestUser(u); err != nil {
			return err
		}
		if err := add(u.Name, u.UID, u.GID); err != nil {
			return err
		}
		for _, g := range u.Groups {
			if !groups[g] {
				return fmt.Errorf("unknown manifest group %q", g)
			}
		}
	}
	return nil
}

func identityCollision(db accountDB, name string, uid, gid int, business bool) string {
	kind := "user"
	recognized := db.user(name)
	hostGID := db.accounts[name].gid
	if business {
		kind = "business group"
		recognized = db.businessGroup(name)
		hostGID = db.groups[name].gid
	}
	if db.taken(name) {
		if !recognized {
			return name + " on this host is not a " + kind
		}
		if id, err := manifestID(db.accounts[name].uid); err != nil || id != uid {
			return fmt.Sprintf("%s on this host has uid %s, not %d", name, db.accounts[name].uid, uid)
		}
		if id, err := manifestID(hostGID); err != nil || id != gid {
			return fmt.Sprintf("%s on this host has gid %s, not %d", name, hostGID, gid)
		}
		return ""
	}
	for _, a := range db.accountOrder {
		if id, err := manifestID(a.uid); err == nil && id == uid {
			return fmt.Sprintf("uid %d of %s is %s's on this host", uid, name, a.name)
		}
	}
	for _, g := range db.groupOrder {
		if id, err := manifestID(g.gid); err == nil && id == gid {
			return fmt.Sprintf("gid %d of %s is %s's on this host", gid, name, g.name)
		}
	}
	return ""
}

// CheckManifest refuses host identity collisions before a restore changes files.
func CheckManifest(_ context.Context, env host.Env, m Manifest) error {
	if err := validateManifest(m); err != nil {
		return err
	}
	db, err := loadAccounts(env.Root)
	if err != nil {
		return err
	}
	m.Groups = slices.Clone(m.Groups)
	m.Users = slices.Clone(m.Users)
	sortManifest(&m)
	for _, g := range m.Groups {
		if reason := identityCollision(db, g.Slug, g.UID, g.GID, true); reason != "" {
			return &RefusedError{Reason: reason}
		}
	}
	for _, u := range m.Users {
		if reason := identityCollision(db, u.Name, u.UID, u.GID, false); reason != "" {
			return &RefusedError{Reason: reason}
		}
	}
	return nil
}
