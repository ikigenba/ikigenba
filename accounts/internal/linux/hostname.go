package linux

import (
	"context"
	"io"
	"os"
	"regexp"
	"strings"
	"syscall"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

const (
	hostnamePath     = "/etc/hostname"
	preserveHostname = "/etc/cloud/cloud.cfg.d/99-ikigenba.cfg"
	maxHostnameBytes = 64
)

var hostnameLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func (h *Host) setHostname(ctx context.Context, s protocol.DesiredState) error {
	if s.Domain == "" {
		return nil
	}
	if len(s.Host) > maxHostnameBytes-1-len(s.Domain) {
		return machine.ErrInvalidState
	}
	name := s.Host + "." + s.Domain
	for label := range strings.SplitSeq(name, ".") {
		if !hostnameLabel.MatchString(label) {
			return machine.ErrInvalidState
		}
	}
	if _, err := h.write(preserveHostname, []byte("preserve_hostname: true\n"), 0o644); err != nil {
		return err
	}
	if h.staticHostname() == name {
		return nil
	}
	_, err := h.run(ctx, "hostnamectl", "set-hostname", name)
	return err
}

func (h *Host) staticHostname() string {
	// Open without blocking so an unexpected FIFO cannot hold up the cycle.
	f, err := os.OpenFile(h.path(hostnamePath), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxCapture {
		return ""
	}
	value, err := io.ReadAll(io.LimitReader(f, MaxCapture))
	if err != nil {
		return ""
	}
	if len(value) == MaxCapture {
		// Recheck the open file in case it grew during the read.
		info, err = f.Stat()
		if err != nil || info.Size() > MaxCapture {
			return ""
		}
	}
	return strings.Trim(string(value), " \t\r\n")
}
