package tools

// ReplaceHostArgs selects the replacement source and optional sizes.
type ReplaceHostArgs struct {
	Host   string  `json:"host" description:"SSH host name." mcp:"required"`
	From   string  `json:"from" description:"Replace from the data volume or backups: volume or backup." mcp:"required"`
	Type   *string `json:"type" description:"EC2 instance type."`
	RootGB *int    `json:"root_gb" description:"Root volume size in GB."`
	DataGB *int    `json:"data_gb" description:"Data volume size in GB."`
}

// BackupStatusArgs selects a window of accounts, optionally on one host.
type BackupStatusArgs struct {
	Host  *string `json:"host" description:"SSH host name."`
	Page  *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// RestoreDatabaseArgs specifies a database and the point in time to restore.
type RestoreDatabaseArgs struct {
	Account string `json:"account" description:"SSH account slug." mcp:"required"`
	Path    string `json:"path" description:"Path relative to the account folder or the user's home." mcp:"required"`
	AsOf    string `json:"as_of" description:"Database restore time in RFC 3339 format." mcp:"required"`
}

// RestoreFilesArgs specifies a snapshot and an optional home and path.
type RestoreFilesArgs struct {
	Account  string  `json:"account" description:"SSH account slug." mcp:"required"`
	User     *string `json:"user" description:"SSH user name."`
	Path     *string `json:"path" description:"Path relative to the account folder or the user's home."`
	Snapshot string  `json:"snapshot" description:"File snapshot time in RFC 3339 format." mcp:"required"`
}

// AccountBackups summarizes backup health and the status of any deletion.
type AccountBackups struct {
	Account        string `json:"account" mcp:"required"`
	Host           string `json:"host" mcp:"required"`
	State          string `json:"state" mcp:"required"`
	Health         string `json:"health" mcp:"required"`
	Snapshot       string `json:"snapshot" mcp:"required"`
	Checked        string `json:"checked" mcp:"required"`
	Error          string `json:"error" mcp:"required"`
	Databases      int    `json:"databases" mcp:"required"`
	DatabaseErrors int    `json:"database_errors" mcp:"required"`
	Synced         string `json:"synced" mcp:"required"`
	Written        string `json:"written" mcp:"required"`
	Final          string `json:"final" mcp:"required"`
	FinalAt        string `json:"final_at" mcp:"required"`
	Restore        string `json:"restore" mcp:"required"`
	Reported       string `json:"reported" mcp:"required"`
	Delete         string `json:"delete" mcp:"required"`
	DeleteReason   string `json:"delete_reason" mcp:"required"`
}

// BackupStatusList holds one window of account backup summaries.
type BackupStatusList struct {
	Accounts []AccountBackups `json:"accounts" mcp:"required"`
	Next     *string          `json:"next" description:"Present when more entries follow; pass it back as page to get them."`
}

// DatabasePoint describes the last reported stream and its restorable range.
type DatabasePoint struct {
	Path    string `json:"path" mcp:"required"`
	Health  string `json:"health" mcp:"required"`
	Oldest  string `json:"oldest" mcp:"required"`
	Written string `json:"written" mcp:"required"`
	Synced  string `json:"synced" mcp:"required"`
	Error   string `json:"error" mcp:"required"`
}

// RestorePoints holds the restore points from an account's latest host report.
type RestorePoints struct {
	Account       string          `json:"account" mcp:"required"`
	Host          string          `json:"host" mcp:"required"`
	Reported      string          `json:"reported" mcp:"required"`
	Snapshots     []string        `json:"snapshots" mcp:"required"`
	Databases     []DatabasePoint `json:"databases" mcp:"required"`
	DatabaseTotal int             `json:"database_total" mcp:"required"`
}

// RestoreMade describes a recorded restore.
type RestoreMade struct {
	Restore string `json:"restore" mcp:"required"`
	Account string `json:"account" mcp:"required"`
	Host    string `json:"host" mcp:"required"`
	Kind    string `json:"kind" mcp:"required"`
	Path    string `json:"path" mcp:"required"`
	User    string `json:"user" mcp:"required"`
	AsOf    string `json:"as_of" mcp:"required"`
	State   string `json:"state" mcp:"required"`
}

// BackupsDeletion shows the state of an account backups delete.
type BackupsDeletion struct {
	Account string `json:"account" mcp:"required"`
	State   string `json:"state" mcp:"required"`
}
