# D06-identity

Every request a service receives has already been authenticated by nginx. nginx asks auth who the caller is and relays the answer to the service in three request headers: `X-User-Id` (the signed-in user's id), `X-User-Email` (their email), and `X-Request-Id` (an id for this request, which every log line about it carries). Package `identity` is the one place a service reads them, so every service treats a missing identity the same way, and the one place a service copies them onto a call it makes to a sibling, so the sibling sees the same caller.

## The trust model

A service listens only on a unix socket that nginx alone can reach, and nginx sets the three headers on every request it passes, replacing whatever the client sent. So a service trusts the headers as given and never checks them against anything. It follows that a request without `X-User-Id` did not come through a correctly configured nginx: it is a misconfiguration of the host, not an unauthenticated user, and there is nothing useful to tell the client. `Require` answers it 500 with a short plain-text body and writes one line naming the request to the service's standard error, so the operator sees it in the journal. It never redirects to sign-in and never answers 401: authentication is nginx's job, and a service that tried to do it would hide the misconfiguration.

## Using it

A service wraps its whole handler tree in `identity.Require`, naming itself (the name its log lines start with) and the writer its diagnostics go to — normally `os.Stderr`. Every handler under it then finds the `Caller` with `identity.FromContext`, and may rely on its `UserID` being non-empty. `X-User-Email` and `X-Request-Id` are carried as given and may be empty; they are not required.

`identity.NewContext` puts a `Caller` into a context directly. It is what `Require` does for a request, exported so that a consumer can call a handler, or code beneath one, with a caller of its choosing without building an HTTP request — a unit test, say.

When a service calls a sibling on the caller's behalf (the MCP gateway calling a backend, D10), `identity.Forward` copies the caller onto the outgoing request's headers, so the sibling's own `Require` sees the same user and the same request id, and the request can be followed through both services' logs.

## REQUIREMENTS

- R-KA5Z-TNTL: Package `identity` MUST export `type Caller struct { UserID, Email, RequestID string }`, with exactly these fields in this order.
- R-KBDW-7FKA: Package `identity` MUST export `const MissingBody = "identity header missing\n"`.
- R-KCLS-L7AZ: Package `identity` MUST export `func Require(app string, stderr io.Writer, next http.Handler) http.Handler`, where `io` and `http` are the standard library's `io` and `net/http`.
- R-KDTO-YZ1O: Package `identity` MUST export `func FromContext(ctx context.Context) (Caller, bool)`, where `context` is the standard library's `context`.
- R-KF1L-CQSD: Package `identity` MUST export `func NewContext(ctx context.Context, c Caller) context.Context`.
- R-KG9H-QIJ2: Package `identity` MUST export `func Forward(c Caller, r *http.Request)`.
- R-KHHE-4A9R: The handler `identity.Require` returns MUST answer a request whose `X-User-Id` header is absent or whose first `X-User-Id` value is empty with status 500, exactly one `Content-Type` header with the value `text/plain; charset=utf-8`, and a body exactly `identity.MissingBody` — an empty body when the method is HEAD — and MUST NOT call `next`.
- R-KIPA-I20G: For each request R-KHHE-4A9R describes, the handler `identity.Require` returns MUST write to `stderr`, in a single `Write` call, exactly one line: `app`, then `: request `, then the request's first `X-Request-Id` value as received or `-` when that header is absent or empty, then `: X-User-Id is missing`, then LF.
- R-KJX6-VTR5: When `stderr` is nil, the handler `identity.Require` returns MUST answer as R-KHHE-4A9R states without panicking and write the line of R-KIPA-I20G nowhere.
- R-KL53-9LHU: The handler `identity.Require` returns MUST call `next` exactly once for a request whose first `X-User-Id` value is not empty, with the same response writer and a request whose context makes `identity.FromContext` return true and the `Caller` whose `UserID` is that value, `Email` the first `X-User-Email` value (empty when absent), and `RequestID` the first `X-Request-Id` value (empty when absent), each unaltered.
- R-KMCZ-ND8J: The request `identity.Require`'s handler passes to `next` MUST be the incoming request with only its context replaced, by one derived from the incoming request's context, so the incoming context's values, deadline, and cancellation remain visible through it.
- R-KNKW-14Z8: For a request R-KL53-9LHU describes, the handler `identity.Require` returns MUST write nothing to `stderr` and nothing to the response beyond what `next` writes.
- R-KOSS-EWPX: `identity.FromContext` MUST return `c` and true for a context returned by `identity.NewContext(ctx, c)`, or derived from one, whatever `c`'s fields hold, the empty `Caller` included; when several `NewContext` calls are in a context's ancestry, the innermost MUST win.
- R-KQ0O-SOGM: `identity.FromContext` MUST return the zero `Caller` and false for a context that neither `identity.NewContext` returned nor derives from one it returned.
- R-KR8L-6G7B: `identity.Forward` MUST set the headers `X-User-Id`, `X-User-Email`, and `X-Request-Id` of `r.Header` to exactly one value each, `c.UserID`, `c.Email`, and `c.RequestID` respectively, replacing every value the header held before, for each of those fields that is not empty.
- R-YLML-I4K4: `identity.Forward` MUST remove from `r.Header` every value of `X-User-Id`, `X-User-Email`, or `X-Request-Id` whose corresponding field of `c` is empty.
- R-YMUH-VWAT: `identity.Forward` MUST change no header of `r` other than `X-User-Id`, `X-User-Email`, and `X-Request-Id`.
- R-KUWA-BRFE: The handler `identity.Require` returns MUST be safe to serve concurrent requests from multiple goroutines.
