package mcp

// Guide is Tier 2 discovery for callers that need repository conventions.
const Guide = `Repos holds four repository kinds: code, sites, scripts, and prompts. Kind defaults to code. Keys are 1-64 lowercase letters, digits, dots, underscores, or hyphens, beginning with a letter or digit.

New repositories use main as their default branch. Clone the URL returned by get, push work on another branch through git, record checks with status_set, then call merge. Check states are pending, success, and failure; pending or failure blocks merge. delete archives history and can be retried; it never destroys the repository.

Examples:
  create {"name":"demo"}
  get {"name":"demo"}
  status_set {"name":"demo","sha":"<commit>","check":"tests","state":"success"}
  merge {"name":"demo","branch":"feature"}
  delete {"kind":"sites","name":"homepage"}`
