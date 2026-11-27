package linux

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

func TestBackupCommandDiagnosticsMaskCredentialValues(t *testing.T) {
	credentials := protocol.Credentials{AccessKeyID: "Key", SecretAccessKey: "KeyExtended", SessionToken: "SessionToken"}
	ctx := withBackupDiagnosticRedactor(t.Context(), &protocol.Backup{Host: credentials})
	const raw = "failure KeyExtended / Key / SessionToken"
	want := "failure " + strings.Join(slices.Repeat([]string{machine.Redacted}, 3), " / ")
	host := New(Env{Execute: func(context.Context, Command) (Result, error) {
		return Result{Stdout: []byte(raw), Stderr: []byte(raw)}, errors.New(raw)
	}})
	result, err := host.query(ctx, Command{Name: ResticPath, Env: backupAWSEnv(credentials)}, 0)
	var failure *CommandError
	if !errors.As(err, &failure) || failure.Err == nil || failure.Err.Error() != want ||
		string(result.Stdout) != want || string(result.Stderr) != want {
		t.Fatalf("diagnostic output = %q / %q / %v", result.Stdout, result.Stderr, err)
	}
}

func TestBackupCapturedDiagnosticsMaskPrefixesOnlyOnKnownCuts(t *testing.T) {
	credentials := protocol.Credentials{AccessKeyID: "Key", SecretAccessKey: "KeyExtended", SessionToken: "SessionToken"}
	ctx := withBackupDiagnosticRedactor(t.Context(), &protocol.Backup{Host: credentials})
	const tokenPrefix = "SessionTo"
	complete := strings.Repeat("x", MaxCapture-len(tokenPrefix)) + tokenPrefix
	for _, tc := range []struct {
		name  string
		feed  string
		want  string
		known bool
	}{
		{name: "exact", feed: complete, want: complete, known: true},
		{name: "cut", feed: complete + "ken", want: strings.TrimSuffix(complete, tokenPrefix) + machine.Redacted, known: true},
		{name: "injected at cap", feed: complete, want: complete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := New(Env{Execute: func(callCtx context.Context, _ Command) (Result, error) {
				var stderr capture
				_, _ = stderr.Write([]byte(tc.feed))
				if tc.known {
					bounds := callCtx.Value(backupCaptureKey{}).(*backupCaptureBounds)
					*bounds = backupCaptureBounds{known: true, stderrCut: stderr.cut}
				}
				return Result{Stderr: stderr.prefix, ExitCode: 1}, nil
			}})
			_, err := host.query(ctx, Command{Name: ResticPath, Env: backupAWSEnv(credentials)}, 0)
			var failure *CommandError
			if !errors.As(err, &failure) || string(failure.Result.Stderr) != tc.want {
				t.Fatal("credential prefix handling differed from the capture outcome")
			}
		})
	}
}

func TestBackupJSONProjectionKeepsSnapshotIDAtLimit(t *testing.T) {
	// R-7U8Y-HYUT
	id := strings.Repeat("x", MaxJournalLine)
	const stamp = "2026-11-26T12:00:00.123456789Z"
	longStamp := strings.TrimSuffix(stamp, "Z") + strings.Repeat("0", MaxJournalLine/2) + "Z"
	want, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	writer := newBackupJSONArray([]string{"time", "id"}, func(row map[string]string) {
		if at, err := time.Parse(time.RFC3339Nano, row["time"]); err == nil && at.Equal(want) {
			ids = append(ids, row["id"])
		}
	})
	for _, chunk := range []string{
		`[{"id":"`, id, `","time":"` + longStamp + `"},`,
		`{"time":"` + longStamp + `","id":"`, id, `"},`,
		`{"time":"` + stamp + `","id":"`, id, `x"},`,
		`{"time":"` + stamp + `","id":"short","unselected":"`, id, `x"}]`,
	} {
		if _, err := io.WriteString(writer, chunk); err != nil {
			t.Fatal(err)
		}
	}
	writer.finish()
	if len(ids) != 3 || ids[0] != id || ids[1] != id || ids[2] != "short" {
		t.Fatal("snapshot projection lost the valid boundary ID or kept the oversized ID")
	}
}

func TestBackupJSONProjectionParsesPaddedTransactionNumbers(t *testing.T) {
	// R-7U8Y-HYUT
	zeros := strings.Repeat("0", MaxJournalLine/2)
	var synced bool
	writer := newBackupJSONArray([]string{"local_txid", "max_txid"}, func(row map[string]string) {
		local, localOK := backupTxID(row["local_txid"])
		maximum, maxOK := backupTxID(row["max_txid"])
		synced = synced || localOK && maxOK && local == 0x10 && maximum >= local
	})
	for _, chunk := range []string{
		`[{"local_txid":"`, zeros, `\u0031\u0030","max_txid":"`, zeros, `10"}]`,
	} {
		if _, err := io.WriteString(writer, chunk); err != nil {
			t.Fatal(err)
		}
	}
	writer.finish()
	if !synced {
		t.Fatal("long zero padding prevented matching hexadecimal transaction numbers")
	}
}
