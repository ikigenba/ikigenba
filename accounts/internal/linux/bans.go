package linux

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

func (h *Host) readBans(ctx context.Context, w *lineWriter) error {
	_, err := h.query(ctx, Command{
		Name: "fail2ban-client", Args: []string{"get", "sshd", "banip", "--with-time"}, Stdout: w,
	}, 0)
	if err == nil {
		w.finish()
	}
	return err
}

func (h *Host) liftBans(ctx context.Context, s protocol.DesiredState, started bool) error {
	for _, address := range s.Unban {
		if _, err := h.run(ctx, "fail2ban-client", "set", "sshd", "unbanip", address); err != nil {
			return err
		}
	}
	if len(s.Allowlist) == 0 || started {
		return nil
	}
	candidates := make([]string, 0, protocol.MaxBans)
	w := lineWriter{limit: MaxBanLine, line: make([]byte, 0, MaxBanLine), emit: func(line []byte) {
		if len(candidates) == cap(candidates) {
			return
		}
		field := line
		if end := bytes.IndexAny(line, " \t"); end >= 0 {
			field = field[:end]
		}
		address, err := valid.Address(string(field))
		if err != nil || address != string(field) {
			return
		}
		for _, entry := range s.Allowlist {
			if valid.Covers(entry, address) {
				candidates = append(candidates, address)
				break
			}
		}
	}}
	if err := h.readBans(ctx, &w); err != nil {
		return err
	}
	for _, address := range candidates {
		if _, err := h.run(ctx, "fail2ban-client", "set", "sshd", "unbanip", address); err != nil {
			return err
		}
	}
	return nil
}

const banTimePattern = `[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}`
const banTimeLayout = "2006-01-02 15:04:05"

var banLinePattern = regexp.MustCompile(`^([^ \t]+)[ \t]+(` + banTimePattern + `) \+ [0-9]+ = (` + banTimePattern + `)$`)

func parseBan(line []byte, location *time.Location) (protocol.Ban, bool) {
	match := banLinePattern.FindSubmatch(line)
	if match == nil {
		return protocol.Ban{}, false
	}
	address, addressErr := valid.Address(string(match[1]))
	start, startErr := time.ParseInLocation(banTimeLayout, string(match[2]), location)
	end, endErr := time.ParseInLocation(banTimeLayout, string(match[3]), location)
	if addressErr != nil || startErr != nil || endErr != nil {
		return protocol.Ban{}, false
	}
	return protocol.Ban{Address: address, Banned: start.UTC(), Expires: end.UTC()}, true
}

func (h *Host) banFacts(ctx context.Context, location *time.Location) ([]protocol.Ban, int) {
	bans := make([]protocol.Ban, 0, protocol.MaxBans)
	w := lineWriter{
		limit: MaxBanLine, line: make([]byte, 0, MaxBanLine), countNonempty: true,
		emit: func(line []byte) {
			ban, ok := parseBan(line, location)
			if !ok {
				return
			}
			index := sort.Search(len(bans), func(i int) bool {
				return ban.Banned.After(bans[i].Banned) ||
					(ban.Banned.Equal(bans[i].Banned) && ban.Address < bans[i].Address)
			})
			if index == cap(bans) {
				return
			}
			if len(bans) < cap(bans) {
				bans = bans[:len(bans)+1]
			}
			copy(bans[index+1:], bans[index:])
			bans[index] = ban
		},
	}
	if err := h.readBans(ctx, &w); err != nil {
		return nil, 0
	}
	if len(bans) == 0 {
		return nil, w.total
	}
	return bans, w.total
}

func (h *Host) appliedAllowlist() []string {
	file, err := os.Open(h.path(jailPath))
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	reader := bufio.NewReader(io.LimitReader(file, MaxCapture))
	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil
		}
		if entries, ok := strings.CutPrefix(strings.TrimSuffix(line, "\n"), "ignoreip ="); ok {
			fields := strings.FieldsFunc(entries, func(r rune) bool { return r == ' ' || r == '\t' })
			fields = slices.DeleteFunc(fields, func(field string) bool { return field == "127.0.0.1/8" || field == "::1" })
			slices.Sort(fields)
			return fields[:min(len(fields), protocol.MaxAllowlist)]
		}
		if err == io.EOF {
			return nil
		}
	}
}
