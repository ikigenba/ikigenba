package linux

import (
	"io"
	"strings"
	"testing"
	"time"
)

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
