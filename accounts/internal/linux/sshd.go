package linux

import (
	"context"
	"errors"
)

const sshUnitPath = "/etc/systemd/system/sshd.service.d/ikigenba.conf"
const sshConfigPath = "/etc/ssh/sshd_config.d/10-ikigenba.conf"

const sshUnitContents = "[Unit]\nRequiresMountsFor=/data/ssh\nRequires=ikigenba-imds.service\nAfter=ikigenba-imds.service\n"

const sshConfigContents = `PubkeyAuthentication yes
PasswordAuthentication no
KbdInteractiveAuthentication no
GSSAPIAuthentication no
GSSAPIKeyExchange no
PermitRootLogin no
LoginGraceTime 30
MaxAuthTries 3
ClientAliveInterval 300
ClientAliveCountMax 2
AllowGroups shell
UsePAM yes
AllowTcpForwarding no
AllowStreamLocalForwarding no
AllowAgentForwarding no
X11Forwarding no
PermitTunnel no
PerSourcePenalties yes
MaxStartups 30:30:100
PerSourceMaxStartups 20
PubkeyAcceptedAlgorithms ssh-ed25519
HostKey /data/ssh/ssh_host_ed25519_key
HostKeyAlgorithms ssh-ed25519
KexAlgorithms mlkem768x25519-sha256,sntrup761x25519-sha512,sntrup761x25519-sha512@openssh.com,curve25519-sha256,curve25519-sha256@libssh.org
Ciphers chacha20-poly1305@openssh.com,aes256-gcm@openssh.com,aes128-gcm@openssh.com
MACs hmac-sha2-256-etm@openssh.com,hmac-sha2-512-etm@openssh.com

Match Group shell
  AuthorizedKeysFile /etc/ssh/authorized_keys/%u
`

func (h *Host) sshd(ctx context.Context) error {
	unit, err := h.write(sshUnitPath, []byte(sshUnitContents), 0o644)
	if err != nil {
		return err
	}
	if unit.changed {
		if _, err := h.run(ctx, "systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	config, err := h.write(sshConfigPath, []byte(sshConfigContents), 0o600)
	if err != nil || !config.changed {
		return err
	}
	if _, err := h.run(ctx, "sshd", "-t", "-f", h.path("/etc/ssh/sshd_config")); err != nil {
		return errors.Join(err, h.restore(sshConfigPath, config))
	}
	if _, err := h.run(ctx, "systemctl", "reload", "sshd.service"); err != nil {
		return errors.Join(err, h.restore(sshConfigPath, config))
	}
	return nil
}
