package repos

import "eventplane/outbox"

// Events is the event registry published by repos. Event families are added
// alongside the v2 domain behavior that emits them.
var Events = outbox.Registry{}
