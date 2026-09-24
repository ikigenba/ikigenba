package cli

// Usage is the complete top-level help text.
const Usage string = "Usage: agent-monitor [options]\n       agent-monitor list <harness>\n       agent-monitor tree <harness> <session-id>\n\nObserve the coding agents on this machine through their logs and hooks.\n\nCommands:\n  list <harness>               list the live root sessions of claude, codex, or grok\n  tree <harness> <session-id>  draw the subagent tree of one session\n\nsee 'agent-monitor <command> --help' for command options\n\nOptions:\n  -h, --help      print this help\n  -V, --version   print the version\n\nExit codes:\n  0  success\n  1  the output could not be written\n  2  usage error\n  3  the harness's session data could not be read\n  4  the session was not found\n"

// ListUsage is the complete help text for list.
const ListUsage string = "Usage: agent-monitor list <harness>\n\nList the live root sessions of one harness, newest activity first.\n\nHarnesses:\n  claude  Claude Code\n  codex   OpenAI Codex CLI\n  grok    Grok Build CLI\n\nOptions:\n  -h, --help  print this help\n"

// TreeUsage is the complete help text for tree.
const TreeUsage string = "Usage: agent-monitor tree <harness> <session-id>\n\nDraw the subagent tree of one session.\n\nHarnesses:\n  claude  Claude Code\n  codex   OpenAI Codex CLI\n  grok    Grok Build CLI\n\nOptions:\n  -h, --help  print this help\n"
