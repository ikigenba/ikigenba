package tools

// Tool descriptions and refusal sentences are shared copy constants.
const (
	MailGuide          = "Mail text reaches the caller; tools carry no attachment bytes, raw message or credential; pull and push record host transfers, transfer shows their outcome, and a shell fetches an artifact at its url."
	DescribeSearch     = "Search mail using query as Gmail's search language passed verbatim."
	DescribeThread     = "Read one page of a thread's messages oldest first."
	DescribeShow       = "Read a full message with an HTML-only body reduced lossily to text."
	DescribeLabels     = "List the mailbox's labels."
	DescribePull       = "Record a transfer of a message or attachment into artifacts."
	DescribeCompose    = "Compose a draft with optional recipients or a reply."
	DescribePush       = "Record a transfer of an artifact onto a draft."
	DescribeSend       = "Send an open draft."
	DescribeDiscard    = "Discard an open draft."
	DescribeDrafts     = "List your stored drafts newest first."
	DescribeDraft      = "Read a draft with its body and current status."
	DescribeTransfers  = "List your transfers newest first."
	DescribeTransfer   = "Read a transfer's status and outcome."
	RefuseNotConnected = "Connect your Google account to continue."
	RefuseReconnect    = "Reconnect your Google account to continue."
	RefuseNotFound     = "The requested item was not found."
	RefuseTooLarge     = "The content exceeds the permitted size."
	RefuseProvider     = "Gmail could not complete the request."
	RefusePath         = "The artifact path is invalid."
	RefuseExpires      = "The expiry is invalid."
	RefuseUnavailable  = "A required service is unavailable."
	RefuseStopping     = "The service is stopping."
	RefuseQueueFull    = "The transfer queue is full."
	RefuseNotOpen      = "The draft is no longer open."
	RefuseBusy         = "The draft has an unfinished transfer."
	RefuseTo           = "The draft needs a recipient."
	RefuseAddress      = "A recipient address is invalid."
)

// Entry describes the typed tool contract.
type Entry struct{ Name, Description string }

// Catalog supplies the public tool contract.
func Catalog() []Entry {
	return []Entry{{"search", DescribeSearch}, {"thread", DescribeThread}, {"show", DescribeShow}, {"labels", DescribeLabels}, {"pull", DescribePull}, {"compose", DescribeCompose}, {"push", DescribePush}, {"send", DescribeSend}, {"discard", DescribeDiscard}, {"drafts", DescribeDrafts}, {"draft", DescribeDraft}, {"transfers", DescribeTransfers}, {"transfer", DescribeTransfer}}
}
