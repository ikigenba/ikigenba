package linux

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

const unitDirectory = "/etc/systemd/system/"

func (h *Host) packages(ctx context.Context) error {
	args := []string{"install", "-y"}
	for _, name := range []string{"nftables", "fail2ban-server", "dnf-automatic"} {
		result, err := h.query(ctx, Command{Name: "rpm", Args: []string{"-q", name}}, 0, 1)
		if err != nil {
			return err
		}
		if result.ExitCode == 1 {
			args = append(args, name)
		}
	}
	if len(args) > 2 {
		_, err := h.run(ctx, "dnf", args...)
		return err
	}
	return nil
}

const imdsRulesPath = "/etc/nftables/ikigenba-imds.nft"
const imdsUnitName = "ikigenba-imds.service"

const imdsRules = `table inet ikigenba_imds
delete table inet ikigenba_imds
table inet ikigenba_imds {
	chain output {
		type filter hook output priority filter; policy accept;
		meta skuid != 0 ip daddr 169.254.169.254 drop
		meta skuid != 0 ip6 daddr fd00:ec2::254 drop
	}
}
`

const imdsUnit = `[Unit]
Before=sshd.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=nft -f /etc/nftables/ikigenba-imds.nft

[Install]
WantedBy=multi-user.target
`

func (h *Host) imds(ctx context.Context) error {
	rules, err := h.write(imdsRulesPath, []byte(imdsRules), 0o644)
	if err != nil {
		return err
	}
	unit, err := h.write(unitDirectory+imdsUnitName, []byte(imdsUnit), 0o644)
	if err != nil {
		return err
	}
	if unit.changed {
		if _, err := h.run(ctx, "systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	result, err := h.query(ctx, Command{Name: "nft", Args: []string{"list", "table", "inet", "ikigenba_imds"}}, 0, 1)
	if err != nil {
		return err
	}
	if rules.changed || result.ExitCode == 1 {
		if _, err := h.run(ctx, "nft", "-f", h.path(imdsRulesPath)); err != nil {
			if rules.changed {
				return errors.Join(err, h.restore(imdsRulesPath, rules))
			}
			return err
		}
	}
	enabled, err := h.unitStatus(ctx, "is-enabled", imdsUnitName)
	if err != nil || enabled {
		return err
	}
	_, err = h.run(ctx, "systemctl", "enable", imdsUnitName)
	return err
}

const journalConfig = "/etc/systemd/journald.conf.d/ikigenba.conf"
const journalDir = "/var/log/journal"
const journalContents = "[Journal]\nStorage=persistent\n"

func (h *Host) journal(ctx context.Context) error {
	dropIn, err := h.write(journalConfig, []byte(journalContents), 0o644)
	if err != nil {
		return err
	}
	created, err := h.ensureJournalDir()
	if err != nil || (!dropIn.changed && !created) {
		return err
	}
	commands := []Command{
		{Name: "systemd-tmpfiles", Args: []string{"--create", "--prefix", h.path(journalDir)}},
		{Name: "systemctl", Args: []string{"restart", "systemd-journald.service"}},
		{Name: "journalctl", Args: []string{"--flush"}},
	}
	for _, command := range commands {
		if _, err := h.query(ctx, command, 0); err != nil {
			if dropIn.changed {
				return errors.Join(err, h.restore(journalConfig, dropIn))
			}
			_, removeErr := h.remove(journalConfig)
			return errors.Join(err, removeErr)
		}
	}
	return nil
}

func (h *Host) ensureJournalDir() (bool, error) {
	info, err := os.Lstat(h.path(journalDir))
	if errors.Is(err, fs.ErrNotExist) {
		return h.dir(journalDir, 0o755)
	}
	if err != nil {
		return false, hostError(journalDir, err)
	}
	if !info.IsDir() {
		return false, hostError(journalDir, fs.ErrInvalid)
	}
	return false, nil
}

const jailPath = "/etc/fail2ban/jail.d/ikigenba.local"

const jailContents = `[sshd]
enabled = true
backend = systemd
mode = aggressive
banaction = nftables-multiport
maxretry = 5
findtime = 10m
bantime = 10m
bantime.increment = true
bantime.maxtime = 1w
`

func (h *Host) fail2ban(ctx context.Context) error {
	const unit = "fail2ban.service"
	jail, err := h.write(jailPath, []byte(jailContents), 0o644)
	if err != nil {
		return err
	}
	enabled, active, err := h.unitReadiness(ctx, unit)
	if err != nil {
		return err
	}
	if !enabled || !active {
		if _, err := h.run(ctx, "systemctl", "enable", "--now", unit); err != nil {
			if jail.changed {
				return errors.Join(err, h.restore(jailPath, jail))
			}
			return err
		}
	}
	if jail.changed && active {
		if _, err := h.run(ctx, "systemctl", "reload", unit); err != nil {
			return errors.Join(err, h.restore(jailPath, jail))
		}
	}
	return nil
}

func (h *Host) unitStatus(ctx context.Context, verb, unit string) (bool, error) {
	result, err := h.query(ctx, Command{Name: "systemctl", Args: []string{verb, unit}})
	return result.ExitCode == 0, err
}

func (h *Host) unitReadiness(ctx context.Context, unit string) (enabled, active bool, err error) {
	enabled, err = h.unitStatus(ctx, "is-enabled", unit)
	if err == nil {
		active, err = h.unitStatus(ctx, "is-active", unit)
	}
	return enabled, active, err
}

func (h *Host) enableAndStart(ctx context.Context, unit string) error {
	enabled, active, err := h.unitReadiness(ctx, unit)
	if err == nil && (!enabled || !active) {
		_, err = h.run(ctx, "systemctl", "enable", "--now", unit)
	}
	return err
}

func (h *Host) shell(ctx context.Context) error {
	exists, err := h.hasShellGroup()
	if err != nil {
		return err
	}
	if !exists {
		if _, err := h.run(ctx, "groupadd", "--system", ShellGroup); err != nil {
			return err
		}
	}
	for _, path := range []string{KeysDir, SkelDir} {
		if _, err := h.dir(path, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (h *Host) hasShellGroup() (bool, error) {
	const path = "/etc/group"
	file, err := os.Open(h.path(path))
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		if strings.HasPrefix(lines.Text(), ShellGroup+":") {
			return true, nil
		}
	}
	if err := lines.Err(); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	return false, nil
}
