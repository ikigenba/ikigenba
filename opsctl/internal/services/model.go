package services

// Change describes how one service entry changed.
type Change string

const (
	// Unchanged denotes no difference from the previously published entry.
	Unchanged Change = "unchanged"
	// Added denotes a newly published entry.
	Added Change = "added"
	// Removed denotes an entry absent from the new document.
	Removed Change = "removed"
	// Disabled denotes an entry changed from enabled to disabled.
	Disabled Change = "disabled"
	// Enabled denotes an entry changed from disabled to enabled.
	Enabled Change = "enabled"
	// Updated denotes other changes to an existing entry.
	Updated Change = "updated"
)

// Changes records the changed entries by service name.
type Changes map[string]Change

// For returns the app's change, or Unchanged when it has no entry.
func (c Changes) For(app string) Change {
	if change, ok := c[app]; ok {
		return change
	}
	return Unchanged
}
