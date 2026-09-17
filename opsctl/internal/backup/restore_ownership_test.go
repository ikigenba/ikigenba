package backup

import "testing"

func TestMappedServiceRestoreOwnershipMapsNamesIndependently(t *testing.T) {
	// R-RY91-HA14
	const (
		sourceUID      = 4242
		sourceGID      = 4343
		destinationUID = 5252
		destinationGID = 5353
	)
	identity := restoreIdentity{needed: true, uid: destinationUID, gid: destinationGID}
	for _, test := range []struct {
		name    string
		entry   serviceRestoreEntry
		wantUID int
		wantGID int
	}{
		{
			name:    "symbolic group preserves numeric owner",
			entry:   serviceRestoreEntry{uid: sourceUID, gid: sourceGID, gname: "ikigenba"},
			wantUID: sourceUID,
			wantGID: destinationGID,
		},
		{
			name:    "symbolic owner preserves numeric group",
			entry:   serviceRestoreEntry{uid: sourceUID, gid: sourceGID, uname: "ikigenba"},
			wantUID: destinationUID,
			wantGID: sourceGID,
		},
		{
			name:    "both symbolic identities map independently",
			entry:   serviceRestoreEntry{uid: sourceUID, gid: sourceGID, uname: "ikigenba", gname: "ikigenba"},
			wantUID: destinationUID,
			wantGID: destinationGID,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			uid, gid := mappedServiceRestoreOwnership(test.entry, identity)
			if uid != test.wantUID || gid != test.wantGID {
				t.Fatalf("mapped ownership = %d:%d, want %d:%d", uid, gid, test.wantUID, test.wantGID)
			}
		})
	}
}
