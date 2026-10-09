package tools

import "github.com/ikigenba/ikigenba/scripts/internal/runner"

// Refusal copy formats.
const (
	MissingScript string = "no script named '%s'"
	MissingRun    string = "no run '%s'"
	InvalidName   string = "invalid name '%s'"
	InvalidRef    string = "invalid ref '%s'"
	InvalidEvent  string = "invalid event '%s'"
	NameTaken     string = "a script named '%s' already exists"
	NoRepository  string = "no repository '%s'"
	NotSubscribed string = "'%s' is not subscribed to '%s'"
	Ended         string = "run '%s' has already ended"
)

// CreateDescription describes creating a script.
const CreateDescription string = "Create a script from one of your repositories and a ref.\n\nname is 1 to 64 lowercase letters, digits, or '-', starting with a letter or digit, is not about, tools, mcp, events, or declarations, and must not already name a script in the space: names are shared by every user, because a script's page is at its name. repo is the id of one of your repositories in repos. ref is the branch, tag, or commit a run uses, main unless given; it is not resolved until a run, so it may name nothing yet. A run unpacks that commit and runs " + runner.Interpreter + " main.py from the repository's root. The script does not run until you call run. The result is what show returns."
