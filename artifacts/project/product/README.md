# artifacts — Product

**Authority: intent.** This doc owns *why* the artifacts service exists, *for
whom*, what is in and out of scope, and the user-facing promises — stated once,
in outcome terms. It does **not** state mechanism, exact paths, formats, exit
codes, schemas, or test assertions; those belong to `project/design/`. Where the
two could overlap (observable behavior), product states the *promise* and design
states the *exact, checkable proof* of that promise. This boundary is
load-bearing and keeps product, design, and plan from restating each other.

## Problem

The suite has no way to hand a file to a person. Files live inside services —
a dropbox mirror, a gmail attachment, a run's sandbox output — and can move
between services over the content plane, but none of that produces a URL a
human can click or share. An owner who wants to store a binary and give someone
a download link has nothing; and an agent that produced or found a file cannot
put it anywhere durable and outward-facing. Worse, an agent cannot ingest a
file from the owner's machine at all without pulling the bytes through its own
context, which the suite forbids.

## Purpose

artifacts is the suite's **file store and distribution point**. It stores
binary files durably and hands out download links for them — public links
anyone can use, or private links requiring a logged-in suite user. Files get in
without ever passing through an agent's context: the agent requests a
short-lived signed upload link and the bytes travel directly from the source
machine to the service, or the service pulls them itself from another suite
service over the content plane. It does one job: **hold files and hand out
links.**

## Users

- **The owner** (an authenticated suite user), through an agent over MCP:
  requests upload links, imports files from other services, lists and inspects
  what is stored, changes a file's visibility or description, deletes files.
- **Link holders** — anyone with a public link, or any logged-in suite user
  with a private link — download files. Public-link holders are not suite users
  and have no identity.
- **Other suite services** pull stored files by reference over the content
  plane, and push their own files in the same way; they never use the human
  links.

## Scope

artifacts **does**:

- Mint **signed upload links** on request: a short-lived, single-use URL that
  accepts one binary file uploaded directly to the service (the agent is told
  to use `curl`; the bytes never enter the agent's context). A link stays
  usable until it expires or the upload succeeds, whichever comes first.
- **Import** a file from another suite service's file interface (dropbox and
  any other content-plane holder), pulled by the service itself by reference.
- Store each file **flat** — no folders — with its filename, an optional
  free-text description, and a visibility of **public** or **private**.
- Hand out a **download link** per file whose address is an unguessable random
  value: public files download for anyone holding the link; private files
  require a logged-in suite user.
- Let the owner **change visibility and description**, and **delete** files;
  stored files otherwise persist until explicitly deleted.
- Count downloads per file (a count only — no per-download log).
- Serve a **landing page** at the mount root for logged-in users: a sortable,
  filterable list of everything stored — filename, visibility, size, creation
  time, download count — with working download links. The page is read-only;
  every mutation goes through MCP.
- Publish an event when a file is **created**, **updated**, or **deleted**, so
  other services can react.
- Expose stored files to other suite services over the content plane.

artifacts does **nothing else**. In particular, for this version it
deliberately excludes:

- **Inline file content over MCP.** No tool accepts file bytes in a call or
  returns them in a result, in any encoding, at any size. Signed links and
  content-plane imports are the only ways in.
- **Folders, versioning, and overwrite.** The namespace is flat; every upload
  is a distinct file; identical filenames coexist as separate entries.
- **Labels/tags.** Filename and description are the only owner-authored
  metadata.
- **Quotas.** No per-owner storage quota; disk is the operator's concern. A
  configurable per-file size cap is the only limit.
- **Auto-expiry of stored files.** Upload links expire; stored files do not.
- **Per-download logging or analytics.** The count is the whole record.
- **Landing-page mutations.** The page lists and links; it never uploads,
  deletes, or edits.

## Contractual constants

- **Starting version `v0.1.0`** — the service's first committed `v`-prefixed
  SemVer `VERSION` (`root project/design/D03.md`), matching the suite
  convention for a brand-new deployable service.
- **External mount `/srv/artifacts/`** — the path prefix under the account
  apex at which the service is reachable, by service-name convention.
- **Upload-link lifetime: 24 hours** — a signed upload link expires 24 hours
  after minting (or on first successful use).

The size cap's default, the link and file address formats, and the event names
are **not** product constants — design declares them.

## What we promise (user-facing behavior)

- **Uploading never touches the agent.** An owner asks their agent to store a
  file; the agent receives a signed upload URL, an expiry time, and a ready-made
  `curl` command, and relays the command to the owner (or runs it in a sandbox).
  The bytes travel directly from the machine holding the file to the service.
- **A signed link is single-use and short-lived.** It accepts exactly one
  successful upload; after that, or after 24 hours, it is dead. A failed
  attempt does not consume it. A dead or unknown link reveals nothing about
  what exists.
- **Filenames are bounded.** A filename longer than the limit is rejected at
  request time — never truncated.
- **Oversize uploads are rejected.** A file exceeding the configured size cap
  is refused and nothing is stored.
- **Every stored file has an unguessable link.** Download addresses embed a
  random value that cannot be guessed or enumerated; knowing one file's link
  reveals nothing about others. The link ends in the file's own name, so what
  downloads is recognizably what was stored.
- **Public means anyone; private means logged-in.** A public file downloads
  for anyone holding its link, no account needed. A private file downloads
  only for a logged-in suite user; a logged-out browser is sent to sign-in,
  not shown an error.
- **Visibility is switchable.** Flipping a file between public and private
  takes effect immediately.
- **Imports work by reference.** An owner can tell the agent to move a file
  from dropbox (or any suite service that exposes files) into artifacts; the
  service fetches the bytes itself, and the stored result behaves exactly like
  an uploaded file.
- **Other services can read stored files.** A stored file is addressable over
  the suite's content plane, so any service can pull it by reference.
- **Deletion is final.** After deletion the file's links are dead and its
  bytes are gone.
- **The landing page shows what is stored.** A logged-in user opening
  `/srv/artifacts/` sees every stored file with its filename, visibility,
  size, creation time, and download count, can sort and filter the list, and
  can download from it. A browser with no session is refused. The page changes
  nothing.
- **Changes are announced.** Creating, updating, or deleting a file publishes
  an event other suite services can act on.

## Success criteria (outcomes)

- An owner, through MCP, receives an upload URL plus a `curl` command; running
  that command from any machine uploads the file, and the file then appears in
  the list with its name, size, and visibility.
- The same upload URL, used a second time, is refused; an upload attempted
  after 24 hours is refused; a failed first attempt leaves the URL usable.
- A request to store a file whose name exceeds the length limit is rejected
  with the name intact — nothing stored, nothing truncated.
- An upload larger than the configured cap is refused and no file appears.
- A public file's link downloads the exact bytes uploaded, with the original
  filename, for a caller with no session or credential.
- A private file's link downloads for a logged-in user; a logged-out browser
  opening it lands on sign-in, and after signing in the user can retrieve the
  file.
- Two files' links share no predictable structure an outsider could use to
  derive one from the other; a made-up link is refused without revealing
  whether anything similar exists.
- After flipping a file public→private, the anonymous download that previously
  succeeded is refused; after flipping back, it succeeds again.
- An owner can import a named file from dropbox by reference; the stored copy's
  bytes match the source exactly, and no file content appeared in any MCP
  result along the way.
- Another suite service can fetch a stored file by reference and receives
  byte-identical content.
- After deleting a file, its download link is dead and its entry is gone from
  the list.
- Each successful download raises that file's download count by one, visible
  in the list.
- A logged-in user can sort the landing-page list (by name, size, time, count)
  and filter it by text, and download a listed file from the page; a
  session-less browser is refused the page.
- A consumer subscribed to the suite's event plane observes a created, an
  updated, and a deleted fact for a file that was uploaded, edited, and
  removed.
