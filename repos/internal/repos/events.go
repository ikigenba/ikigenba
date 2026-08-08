package repos

import "eventplane/outbox"

type PushPayload struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Branch string `json:"branch"`
	SHA    string `json:"sha"`
	OldSHA string `json:"old_sha"`
	Actor  string `json:"actor"`
}

type ArchivedPayload struct {
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	ArchivedPath string `json:"archived_path"`
}

var Events = outbox.Registry{
	{Kind: "push", Subject: "/<kind>/<name>", Description: "a branch of a repository moved", Sample: PushPayload{Kind: "code", Name: "demo", Branch: "main", SHA: "0123456789012345678901234567890123456789", OldSHA: "", Actor: "owner@example.com"}},
	{Kind: "archived", Subject: "/<kind>/<name>", Description: "a repository was archived", Sample: ArchivedPayload{Kind: "code", Name: "demo", ArchivedPath: "archive/code/demo.1.git"}},
}
