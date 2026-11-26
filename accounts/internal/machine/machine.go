// Package machine defines the host operations used by the daemon.
package machine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

// Machine reconciles the host, reports its state and replaces the daemon binary.
type Machine interface {
	Apply(ctx context.Context, s protocol.DesiredState) error
	Inspect(ctx context.Context, s protocol.DesiredState) (protocol.Report, error)
	InstallDaemon(ctx context.Context, body io.Reader, digest string) error
}

// SeedMessage is the first commit's message in a new account folder.
const SeedMessage = "Initialize account folder"

const (
	noDataVolumeText   = "data volume is not attached"
	digestMismatchText = "daemon digest does not match"
	notExecutableText  = "daemon is not an ELF executable"
	tooLargeText       = "daemon exceeds the maximum binary size"
	identityText       = "host account or group conflicts with desired identity"
	invalidStateText   = "invalid desired host state"
)

// Errors returned by reconciliation and daemon verification.
var (
	ErrNoDataVolume   = errors.New(noDataVolumeText)
	ErrDigestMismatch = errors.New(digestMismatchText)
	ErrNotExecutable  = errors.New(notExecutableText)
	ErrTooLarge       = errors.New(tooLargeText)
	ErrIdentity       = errors.New(identityText)
	ErrInvalidState   = errors.New(invalidStateText)
)

// VerifyDaemon streams and verifies a daemon into dst. A nil dst verifies and
// discards the entire body for an in-memory machine.
func VerifyDaemon(ctx context.Context, dst io.Writer, body io.Reader, digest string) error {
	var buffer [protocol.DigestBuffer]byte
	var header [4]byte
	hash := sha256.New()
	total := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunk := buffer[:]
		if dst != nil {
			chunk = chunk[:min(len(chunk), protocol.MaxBinaryBytes-total+1)]
		}
		n, err := body.Read(chunk)
		if total < len(header) {
			copy(header[total:], chunk[:n])
		}
		total += n
		if total > protocol.MaxBinaryBytes && (dst != nil || err != nil) {
			return ErrTooLarge
		}
		if total <= protocol.MaxBinaryBytes {
			_, _ = hash.Write(chunk[:n])
			if dst != nil && n > 0 {
				if _, writeErr := dst.Write(chunk[:n]); writeErr != nil {
					return writeErr
				}
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	if header != [4]byte{0x7f, 'E', 'L', 'F'} {
		return ErrNotExecutable
	}
	if protocol.DigestPrefix+hex.EncodeToString(hash.Sum(nil)) != digest {
		return ErrDigestMismatch
	}
	return nil
}
