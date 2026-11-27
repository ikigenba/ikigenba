package pages

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
)

// Handler answers browser requests without changing the record.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, ok := identity.FromContext(r.Context())
		if !ok || caller.UserID == "" {
			redirect(w, http.StatusFound, SignIn(r, cfg.ServicesPath))
			return
		}
		auth := Auth(r, cfg.ServicesPath)
		banner := cfg.Banner(page.User{Email: caller.Email, ProfileURL: auth + "/", LogoutURL: auth + "/logout"})
		banner.Trail = nil
		escaped := r.URL.EscapedPath()
		host, hasHost := strings.CutPrefix(escaped, "/hosts/")
		hasHost = hasHost && host != "" && !strings.Contains(host, "/")
		notFound := func() { cfg.Pages.Write(w, r, http.StatusNotFound, "notfound", NotFoundData{Banner: banner}) }
		if !hasHost && escaped != "/" && escaped != "/tools" && escaped != "/about" {
			notFound()
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch escaped {
		case "/":
			landing(w, r, cfg, banner, notFound)
		case "/tools":
			banner.Trail = []page.Level{ToolsLevel}
			catalog := cfg.MCP.Tools()
			rows := make([]ToolRow, 0, len(catalog))
			for _, t := range catalog {
				rows = append(rows, ToolRow{Name: t.Name, Description: tools.FirstLine(t.Description)})
			}
			cfg.Pages.Write(w, r, http.StatusOK, "tools", ToolsData{Banner: banner, Tools: rows})
		case "/about":
			about(w, r, cfg, banner)
		default:
			hostDetail(w, r, cfg, banner, host, notFound)
		}
	})
}

func about(w http.ResponseWriter, r *http.Request, cfg Config, banner page.Banner) {
	settings, err := cfg.Store.BackupSettings(r.Context())
	if err != nil {
		refusal(w, r, store.Unreachable)
		return
	}
	banner.Trail = []page.Level{AboutLevel}
	cfg.Pages.Write(w, r, http.StatusOK, "about", AboutData{
		Banner: banner, Description: Description, DefaultType: valid.DefaultType,
		DefaultRootGB: valid.DefaultRootGB, DefaultDataGB: valid.DefaultDataGB,
		BackupEveryMinutes: settings.EveryMinutes, BackupHourly: settings.Hourly,
		BackupDaily: settings.Daily, BackupWeekly: settings.Weekly, BackupWindowDays: settings.WindowDays,
		BackupFilesStaleMinutes: settings.FilesStaleMinutes, BackupDatabaseStaleMinutes: settings.DatabaseStaleMinutes,
	})
}

func landing(w http.ResponseWriter, r *http.Request, cfg Config, banner page.Banner, notFound func()) {
	data := LandingData{Banner: banner, Configured: cfg.Configured}
	if !cfg.Configured {
		cfg.Pages.Write(w, r, http.StatusOK, "landing", data)
		return
	}
	positions := readPositions(r.URL.Query())
	if !positions.valid() {
		notFound()
		return
	}
	data.Domain = cfg.Domain
	var volumes []store.KeptVolume
	var users []store.User
	var hw, vw, aw, uw window
	ctx := r.Context()
	err := store.ReadSnapshot(ctx, cfg.Store, func(snapshot *store.Snapshot) error {
		settings, err := snapshot.BackupSettings(ctx)
		if err != nil {
			return err
		}
		var hosts []store.Host
		var accounts []store.Account
		hosts, hw, err = hostListing(snapshot).read(ctx, positions.hosts, positions.hostSeq)
		if err != nil {
			return err
		}
		volumes, vw, err = volumeListing(snapshot).read(ctx, positions.volumes, positions.volume)
		if err != nil {
			return err
		}
		accounts, aw, err = accountListing(snapshot).read(ctx, positions.accounts, positions.slug)
		if err != nil {
			return err
		}
		users, uw, err = userListing(snapshot).read(ctx, positions.users, userKey{positions.userAccount, positions.userName})
		if err != nil || hw.underflow || vw.underflow || aw.underflow || uw.underflow {
			return err
		}
		return landingBackupRows(ctx, snapshot, &data, hosts, accounts, settings, cfg.Now())
	})
	if err != nil {
		refusal(w, r, store.Unreachable)
		return
	}
	if hw.underflow || vw.underflow || aw.underflow || uw.underflow {
		redirect(w, http.StatusSeeOther, canonicalLanding(hw, aw, uw, vw))
		return
	}
	data.KeptVolumes = make([]KeptVolumeRow, 0, len(volumes))
	for _, v := range volumes {
		data.KeptVolumes = append(data.KeptVolumes, keptVolumeRow(v))
	}
	data.Users = make([]UserRow, 0, len(users))
	for _, u := range users {
		data.Users = append(data.Users, userRow(u))
	}
	data.HostsPage, data.HostsPrev, data.HostsNext = hw.value, hw.prev, hw.next
	data.VolumesPage, data.VolumesPrev, data.VolumesNext = vw.value, vw.prev, vw.next
	data.AccountsPage, data.AccountsPrev, data.AccountsNext = aw.value, aw.prev, aw.next
	data.UsersPage, data.UsersPrev, data.UsersNext = uw.value, uw.prev, uw.next
	cfg.Pages.Write(w, r, http.StatusOK, "landing", data)
}

func hostDetail(w http.ResponseWriter, r *http.Request, cfg Config, banner page.Banner, name string, notFound func()) {
	if !cfg.Configured {
		notFound()
		return
	}
	data := HostData{Banner: banner}
	ctx := r.Context()
	err := store.ReadSnapshot(ctx, cfg.Store, func(snapshot *store.Snapshot) error {
		h, err := snapshot.Host(ctx, name)
		if err != nil {
			return err
		}
		report, err := snapshot.Report(ctx, name)
		if err != nil {
			return err
		}
		settings, err := snapshot.BackupSettings(ctx)
		if err != nil {
			return err
		}
		facts, err := snapshot.BackupsOn(ctx, name)
		if err != nil {
			return err
		}
		accounts, err := snapshot.AccountsOn(ctx, name)
		if err != nil {
			return err
		}
		restores, total, err := snapshot.RestoresOn(ctx, name, MaxRestoreRows)
		if err != nil {
			return err
		}
		now := cfg.Now()
		data.Banner.Trail = []page.Level{HostLevel(h.Name)}
		data.Host = hostView(h, cfg.Domain, now, flaggedBackups(facts, settings, now))
		data.Report = reportView(report, h.Version)
		data.Backups, data.DatabaseCount = backupRows(facts, accounts, report, settings, now)
		data.Restores, data.RestoreTotal = restoreRows(restores), total
		return nil
	})
	if errors.Is(err, store.ErrNotFound) {
		notFound()
		return
	}
	if err != nil {
		refusal(w, r, store.Unreachable)
		return
	}
	cfg.Pages.Write(w, r, http.StatusOK, "host", data)
}

func redirect(w http.ResponseWriter, status int, location string) {
	w.Header().Set("Location", location)
	w.WriteHeader(status)
}

func refusal(w http.ResponseWriter, r *http.Request, line string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(line + "\n"))
	}
}
