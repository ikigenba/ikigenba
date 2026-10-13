# D25-paging

Two suite rules shape every list a service offers. First, every tool or page that lists data an agent or a person can add to pages it, and pages it with one shape, so an agent that has paged one list already knows how to page the next. Second, a big list is filtered, ordered and windowed by the database: the query carries the filter in its WHERE clause, the order in its ORDER BY, the window in its LIMIT, an index backs the order, and the service never holds more than one page plus one row. A list whose size configuration bounds, such as the services in the services file or the tools a server registers, is listed whole and is not paged.

Package `paging` holds the small part of that shape a library can hold: the opaque token that marks where the next page starts, the reading of a requested page size, and the shared words a tool shows a model. The rest is each app's own behavior, which appkit's tests cannot reach, so it is stated here as the suite convention and each app's design states its own requirements, citing this document.

## The wire shape

A paged tool takes two optional members on its input: `page`, an opaque string, and `limit`, an integer. Its answer is the collection member it already has (`widgets`, `runs`, and so on), followed by `next`, an opaque string present exactly when at least one entry follows the page. An absent or empty `page` asks for the first page, and the caller hands `next` back as `page` unchanged to get the one after. `limit` runs from 1 to `MaxLimit`, and is `DefaultLimit` when absent. A `page` the service did not issue is refused before the store is read, with the sentence `BadPage`; a `limit` outside the range is refused the same way, with the sentence `BadLimit` formats. A `limit` too large for a Go `int` never reaches the tool: appkit/mcp refuses it as an integer-range offence (D09).

The two input members are declared on each tool's own input struct as pointer fields, `Page *string` with the JSON name `page` and `Limit *int` with the JSON name `limit`, and `next` is `Next *string` with the JSON name `next` on the output, so a nil `Next` leaves the member out (D09). Each field carries a `description` struct tag, which D09 copies into the schema. Nothing is embedded and appkit/mcp's schema derivation is unchanged. A Go struct tag is a literal, so a tool repeats the words of `PageDescription`, `LimitDescription` and `NextDescription` in its tags, and its own test compares the description `tools/list` returns with the constant. That is why each of these constants is safe to repeat inside a tag: none holds a double quote, a backquote, a backslash or a control character. What each conveys: `page` continues a previous answer with the same filters; `limit` is how many entries to return, within the range, `DefaultLimit` when left out; `next` is present when more entries follow and is passed back as `page`.

A token does not bind the filters. A page continues an answer with the same filters, and keeping them the same is the caller's responsibility; a token handed to the same tool with other filters resumes from the same position in the new answer. The trail is unchanged: reading a page is a tool call like any other, recorded by D08's `tool.called`, which carries no arguments, so the token appears in no event and no log line. Paging itself writes nothing and records nothing.

## The token

A `Token` is a value bound to the tool that issues it, such as `paging.Token{Tool: "list_widgets"}`. `Encode` turns the key of the last entry on a page, a list of `int64` and `string` fields in the order the query sorts by, into the string the answer carries as `next`; `Decode` turns a `page` back into that key, or refuses it with `ErrToken`, whose text is `BadPage`. The tool then reads the rows after that key with a keyset condition, so a page costs the same however deep it is.

The framing is fixed in the contract, not left to the implementation, because a token one release issued must decode in the next during a rolling restart. The payload is each field in order, a kind byte then its value: an `int64` as eight big-endian bytes, a string as its four-byte big-endian length then its bytes. A CRC-32 checksum covers the tool's name and the payload, so a token issued by one tool is refused by another and a mistyped token is refused rather than misread; the tool's name itself does not travel. The token is the payload then the checksum, written in unpadded base64url. Decoding is canonical: a string is accepted only when encoding the fields it decodes to gives back the same string, so each position has exactly one token. A string field longer than a four-byte length can hold is not supported.

Encode and Decode panic on a programmer's mistake, a missing field or a field of another type, as appkit/mcp's registration does; such a mistake surfaces in the tool's first test, never as an answer to a model.

## The window

`Window` reads the requested `limit`: nil is `DefaultLimit`, a value from 1 to `MaxLimit` is itself, and anything else is an error whose text is `BadLimit` formatted with the minimum, the maximum and the offending value, so the bounds come from the constants rather than from words. The error is a value whose `Error()` is that formatted text and nothing else. The tool asks its store for one more row than the window, answers the first window's worth, and, when the extra row came back, sets `next` to the token of the last entry it answered.

## Out of scope

A paged iterator on appkit/mcp's client, a `count` of matching entries, and paging of appkit/mcp's own `tools/list` are deferred and not designed here.

## REQUIREMENTS

- R-QPUC-U9RQ: Package `paging` MUST export `type Token struct { Tool string }`, with exactly this one field.
- R-QR29-81IF: Package `paging` MUST export the method `func (t Token) Encode(fields ...any) string`.
- R-QSA5-LT94: Package `paging` MUST export the method `func (t Token) Decode(token string, into ...any) error`.
- R-QTI1-ZKZT: Package `paging` MUST export `var ErrToken error`, non-nil.
- R-QUPY-DCQI: Package `paging` MUST export `func Window(limit *int) (int, error)`.
- R-QVXU-R4H7: Package `paging` MUST export the untyped integer constant `DefaultLimit = 25`.
- R-QX5R-4W7W: Package `paging` MUST export the untyped integer constant `MaxLimit = 500`.
- R-QYDN-INYL: Package `paging` MUST export the copy constants `const BadPage string`, `const BadLimit string`, `const PageDescription string`, `const LimitDescription string` and `const NextDescription string`, whose values are copy and not contract.
- R-QZLJ-WFPA: `Token.Encode` MUST panic when given no fields, and when any field's dynamic type is not exactly `int64` or `string`, so that a field of type `int`, `int32`, a named type whose underlying type is `int64` or `string`, `*int64`, or nil makes it panic.
- R-R21C-NZ6O: `Token.Decode` MUST panic, whatever `token` is, when `into` is empty, and when any element of `into` is not a non-nil pointer of type `*int64` or `*string`.
- R-R399-1QXD: The payload of `Token.Encode` MUST be the concatenation, for each field in order, of one kind byte followed by the field's value: for an `int64` the byte 0x01 followed by its 8-byte big-endian two's-complement value, and for a `string` the byte 0x02 followed by its byte length as a 4-byte big-endian unsigned integer and then its bytes.
- R-R4H5-FIO2: `Token.Encode` MUST return the `encoding/base64.RawURLEncoding` encoding (base64url without padding) of its payload followed by a 4-byte big-endian checksum, where the checksum is `hash/crc32.ChecksumIEEE` (CRC-32, IEEE polynomial) of the byte length of `t.Tool` as a 4-byte big-endian unsigned integer, followed by the bytes of `t.Tool`, followed by the payload.
- R-R5P1-TAER: `Token.Decode` MUST return nil exactly when `token` equals `t.Encode(v1, ..., vn)` for values `v1` to `vn` whose count equals the length of `into` and each of which is an `int64` where the matching element of `into` is an `*int64` and a `string` where it is a `*string`, and when it returns nil it MUST have stored each `vi` through the `i`th element of `into`.
- R-R6WY-725G: When `token` is not as R-R5P1-TAER accepts, `Token.Decode` MUST return `ErrToken` itself, so that both `err == ErrToken` and `errors.Is(err, ErrToken)` hold, and MUST leave the value every element of `into` points to unchanged.
- R-J11A-W7NQ: `Token.Decode` MUST return `ErrToken` for each of: the empty string; a string that is not valid base64url without padding, including one with padding, with standard-alphabet `+` or `/`, with unused trailing bits set, or containing a carriage return, a line feed or any other character outside the base64url alphabet; a string whose decoded bytes are fewer than four; a token `Encode` returned for a `Token` whose `Tool` differs from `t.Tool`, unless the checksums the two `Tool` values give for it are equal; a token whose last four decoded bytes differ from the checksum of the rest; a token `Encode` returned whose fields are fewer or more than the elements of `into`, or whose kind at some position differs from that element's; a token `Encode` returned with one byte appended to its decoded bytes, or with its last decoded byte removed; and, among strings whose decoded bytes end in the checksum of the rest, one whose payload holds a kind byte other than 0x01 and 0x02, one whose payload holds the kind byte 0x01 followed by fewer than eight bytes, and one whose payload holds the kind byte 0x02 followed by a length, such as 0xFFFFFFFF, greater than the number of bytes that follow it.
- R-J3H3-NR54: When every element of `into` is a non-nil `*int64` or `*string`, `Token.Decode` MUST return without panicking for every `token` value, each string R-J11A-W7NQ lists included.
- R-R9CQ-YLMU: `ErrToken.Error()` MUST return exactly `BadPage`.
- R-RAKN-CDDJ: `Window(nil)` MUST return `DefaultLimit` and a nil error.
- R-RBSJ-Q548: When `*limit` is from 1 to `MaxLimit` inclusive, `Window(limit)` MUST return `*limit` and a nil error.
- R-RD0G-3WUX: When `limit` is non-nil and `*limit` is less than 1 or greater than `MaxLimit`, `Window(limit)` MUST return 0 and a non-nil error whose `Error()` is exactly `fmt.Sprintf(BadLimit, 1, MaxLimit, *limit)`.
- R-J4P0-1IVT: When `Window(limit)` returns a non-nil error, that error's `Error()` MUST contain the decimal form of `*limit` and the decimal form of `MaxLimit`, and MUST NOT contain `%!`.
- R-RFG8-VGCB: Each of `BadPage`, `BadLimit`, `PageDescription`, `LimitDescription` and `NextDescription` MUST be non-empty and MUST contain no double quote, backquote, backslash, or control character (no rune below U+0020 and no U+007F).
- R-RGO5-9830: An exported function or method of package `paging` MUST NOT write to the process's standard output, to its standard error, or through the standard library `log` package's default logger.
- R-RHW1-MZTP: Every exported function and method of package `paging` MUST behave identically whatever the process working directory is.
- R-RJ3Y-0RKE: `Token.Encode`, `Token.Decode` and `Window` MUST be safe for concurrent use: calls from many goroutines at once, on the same `Token` value or on copies of it, MUST each return what the same call made alone returns, with no data race.
