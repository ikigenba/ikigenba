package browse

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/chat"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/session"
	"github.com/ikigenba/ikigenba/agent-monitor/internal/tree"
)

type browseChatDecoder struct {
	calls int
	roots []fs.FS
}

func (d *browseChatDecoder) Decode(root fs.FS, raw []byte) ([]chat.Entry, chat.Usage) {
	d.calls++
	d.roots = append(d.roots, root)
	return []chat.Entry{{Kind: chat.KindUser, Text: string(raw)}}, chat.Usage{In: 1}
}
func chatWithEntries(entries ...chat.Entry) *chatState {
	return &chatState{transcript: chat.NewTranscript("", chat.Recorded{}, nil), entries: entries, follows: true}
}
func chatBodyStrings(body []renderLine) []string {
	result := make([]string, len(body))
	for i, row := range body {
		result[i] = row.text
	}
	return result
}
func longChat() *chatState {
	return chatWithEntries(chat.Entry{Kind: chat.KindUser, Text: strings.Repeat("a", 250) + "\nsecond\nthird\nfourth\nfifth"})
}

// R-DCFC-PP6M
func TestBrowseChatReadDispatch(t *testing.T) {
	root := fstest.MapFS{"log": &fstest.MapFile{Data: []byte("{}\n")}}
	c := newChatState()
	calls := 0
	fail := true
	d := &browseChatDecoder{}
	open := func(got fs.FS) (*chat.Transcript, []chat.Entry, error) {
		calls++
		if !reflect.DeepEqual(got, root) {
			t.Fatal("wrong root")
		}
		if fail {
			return nil, nil, fs.ErrPermission
		}
		if calls == 3 {
			return chat.NewTranscript("", chat.Recorded{}, nil), nil, nil
		}
		return chat.NewTranscript("/log", chat.Recorded{}, func() chat.Decoder { return d }), nil, nil
	}
	for range 2 {
		if err := c.read(root, open); !errors.Is(err, fs.ErrPermission) {
			t.Fatal(err)
		}
	}
	fail = false
	for range 2 {
		if err := c.read(root, open); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 4 {
		t.Fatalf("opening calls %d", calls)
	}
	for range 2 {
		if err := c.read(root, open); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 4 || d.calls != 1 || !reflect.DeepEqual(d.roots[0], root) {
		t.Fatalf("calls=%d decoder=%d", calls, d.calls)
	}
}

// R-AZBT-NF2C
func TestBrowseChatAccumulation(t *testing.T) {
	root := fstest.MapFS{"log": &fstest.MapFile{Data: []byte("{}\n"), Sys: &syscall.Stat_t{Ino: 1}}}
	d := &browseChatDecoder{}
	tr := chat.NewTranscript("/log", chat.Recorded{In: true}, func() chat.Decoder { return d })
	empty := chat.NewTranscript("", chat.Recorded{}, nil)
	c := newChatState()
	errSentinel := errors.New("failed")
	if err := c.read(root, func(fs.FS) (*chat.Transcript, []chat.Entry, error) {
		return empty, []chat.Entry{{Text: "excluded"}}, errSentinel
	}); !errors.Is(err, errSentinel) || c.hasContent() || len(c.entries) != 0 {
		t.Fatal("failed opening changed state")
	}
	initial := []chat.Entry{{Kind: chat.KindAgent, Text: "first"}, {Kind: chat.KindUser, Text: "second"}}
	if err := c.read(root, func(fs.FS) (*chat.Transcript, []chat.Entry, error) { return empty, initial, nil }); err != nil {
		t.Fatal(err)
	}
	if err := c.read(root, func(fs.FS) (*chat.Transcript, []chat.Entry, error) {
		return tr, []chat.Entry{{Kind: chat.KindAssistant, Text: "third"}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	noOpen := func(fs.FS) (*chat.Transcript, []chat.Entry, error) { t.Fatal("unexpected open"); return nil, nil, nil }
	if err := c.read(root, noOpen); err != nil {
		t.Fatal(err)
	}
	root["log"] = &fstest.MapFile{Data: []byte("{\"x\":1}\n"), Sys: &syscall.Stat_t{Ino: 2}}
	if err := c.read(root, noOpen); err != nil {
		t.Fatal(err)
	}
	want := []string{"first", "second", "third", "{}", "{\"x\":1}"}
	var got []string
	for _, entry := range c.entries {
		got = append(got, entry.Text)
	}
	if !reflect.DeepEqual(got, want) || c.transcript != tr || tr.Usage().In != 1 {
		t.Fatalf("entries %v transcript %p usage %+v", got, c.transcript, tr.Usage())
	}
	reopened := newChatState()
	if err := reopened.read(root, func(fs.FS) (*chat.Transcript, []chat.Entry, error) { return empty, initial, nil }); err != nil || !reflect.DeepEqual(reopened.entries, initial) {
		t.Fatal("reopen retained prior entries")
	}
}

// R-VPE2-NQ9H
func TestBrowseChatLayout(t *testing.T) {
	c := chatWithEntries(chat.Entry{Kind: chat.KindAgent, Text: "one"}, chat.Entry{Kind: chat.KindAssistant, Text: "two"})
	l := c.layout(120, 9)
	want := []string{"- agent", "one", "", "- assistant", "two", ""}
	if !reflect.DeepEqual(l.rows, want) {
		t.Fatalf("rows %q", l.rows)
	}
	c.entries = []chat.Entry{{Kind: chat.KindUser, Text: strings.Repeat("é", 250)}}
	l = c.layout(120, 9)
	if !reflect.DeepEqual(l.rows, []string{"- user", strings.Repeat("é", 120), strings.Repeat("é", 120), strings.Repeat("é", 10), ""}) {
		t.Fatalf("wrapped %q", l.rows)
	}
	totals := strings.TrimSuffix(chat.TotalsLine(c.transcript.Usage(), c.transcript.Recorded()), "\n")
	for _, h := range []int{5, 6, 7} {
		l = c.layout(40, h)
		wantTotals := wrapLine(totals, 40)
		if len(wantTotals) > h-4 {
			wantTotals = []string{totals}
		}
		if !reflect.DeepEqual(l.totals, wantTotals) {
			t.Fatalf("height %d totals %q", h, l.totals)
		}
	}
	c.entries = nil
	if len(c.layout(120, 9).rows) != 0 {
		t.Fatal("empty chat has rows")
	}
}

// R-B1RM-EYJQ
func TestBrowseChatPageAndEnd(t *testing.T) {
	for _, tc := range []struct{ lines, page, end int }{{0, 5, 0}, {2, 5, 0}, {5, 5, 0}, {6, 5, 1}, {12, 5, 7}} {
		c := chatWithEntries()
		if tc.lines == 2 {
			c.entries = []chat.Entry{{Kind: chat.KindResultOK}}
		} else if tc.lines > 2 {
			c.entries = []chat.Entry{{Kind: chat.KindUser, Text: strings.Repeat("x\n", tc.lines-3) + "x"}}
		}
		l := c.layout(120, 9)
		if len(l.rows) != tc.lines || l.page != tc.page || l.end != tc.end {
			t.Fatalf("%+v: rows %d page=%d end=%d", tc, len(l.rows), l.page, l.end)
		}
	}
}

// R-B2ZI-SQAF
func TestBrowseChatBodySelection(t *testing.T) {
	c := chatWithEntries(chat.Entry{Kind: chat.KindAgent, Text: "one"}, chat.Entry{Kind: chat.KindAssistant, Text: "two"})
	totals := strings.TrimSuffix(chat.TotalsLine(c.transcript.Usage(), c.transcript.Recorded()), "\n")
	want := []string{"one", "", "- assistant", "two", "", totals}
	if got := chatBodyStrings(c.body(120, 9)); !reflect.DeepEqual(got, want) {
		t.Fatalf("following body %q", got)
	}
	c.scroll(keyFirst, 120, 9)
	want = []string{"- agent", "one", "", "- assistant", "two", totals}
	if got := chatBodyStrings(c.body(120, 9)); !reflect.DeepEqual(got, want) {
		t.Fatalf("paused body %q", got)
	}
	c = chatWithEntries(chat.Entry{Kind: chat.KindResultOK})
	if got := chatBodyStrings(c.body(120, 9)); !reflect.DeepEqual(got, []string{"- result ok", "", totals}) {
		t.Fatalf("fitting body %q", got)
	}
}

// R-B47F-6I14
func TestBrowseChatStartsAndStaysFollowing(t *testing.T) {
	if !newChatState().following() {
		t.Fatal("opening paused")
	}
	c := longChat()
	for _, size := range [][2]int{{120, 9}, {40, 7}, {120, 12}} {
		l := c.layout(size[0], size[1])
		body := c.body(size[0], size[1])
		if len(l.rows) > 0 && body[0].text != l.rows[l.end] || !c.following() {
			t.Fatalf("size %v not at end", size)
		}
		c.entries = append(c.entries, chat.Entry{Kind: chat.KindUser, Text: "gained"})
	}
}

// R-UKQ1-4N11
func TestBrowseChatPausedAnchor(t *testing.T) {
	c := longChat()
	c.scroll(keyFirst, 40, 7)
	c.scroll(keyDown, 40, 7)
	c.scroll(keyDown, 40, 7)
	anchor := c.anchor
	if anchor != (chatAnchor{line: 1, offset: 40}) || c.following() {
		t.Fatalf("anchor %+v", anchor)
	}
	before := chatBodyStrings(c.body(40, 7))
	root := fstest.MapFS{}
	if err := c.read(root, func(fs.FS) (*chat.Transcript, []chat.Entry, error) {
		return c.transcript, []chat.Entry{{Kind: chat.KindUser, Text: "new"}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, chatBodyStrings(c.body(40, 7))) || c.anchor != anchor {
		t.Fatal("append moved view")
	}
	_ = renderScreen(screenView{}, 39, 7)
	if c.anchor != anchor || c.following() {
		t.Fatal("too small changed anchor")
	}
	got := c.body(80, 7)
	if got[0].text != strings.Repeat("a", 80) || c.anchor != anchor || c.following() {
		t.Fatal("resize lost line anchor")
	}
	_ = c.body(120, 50)
	if !c.following() || c.anchor != anchor {
		t.Fatal("resize fitting end did not resume")
	}
}

// R-UNWG-VZFT
func TestBrowseChatScrollTargets(t *testing.T) {
	for _, k := range []key{keyUp, keyDown, keyPageUp, keyPageDown, keyFirst, keyLast} {
		c := longChat()
		l := c.layout(40, 9)
		target := l.end
		switch k {
		case keyUp:
			target--
		case keyDown:
			target++
		case keyPageUp:
			target -= l.page
		case keyPageDown:
			target += l.page
		case keyFirst:
			target = 0
		case keyLast:
			target = l.end
		}
		target = max(0, min(target, l.end))
		c.scroll(k, 40, 9)
		if c.following() != (target == l.end) || !c.following() && c.anchor != l.anchors[target] {
			t.Fatalf("key %v target %d anchor %+v", k, target, c.anchor)
		}
		fit := chatWithEntries()
		fit.scroll(k, 120, 9)
		if !fit.following() {
			t.Fatalf("fitting key %v paused", k)
		}
	}
	for _, tc := range []struct {
		k      key
		target int
	}{{keyUp, 1}, {keyDown, 3}, {keyPageUp, 0}, {keyPageDown, 6}, {keyFirst, 0}, {keyLast, 9}} {
		c := longChat()
		c.scroll(keyFirst, 40, 9)
		c.scroll(keyDown, 40, 9)
		c.scroll(keyDown, 40, 9)
		l := c.layout(40, 9)
		c.scroll(tc.k, 40, 9)
		if c.following() != (tc.target == 9) || !c.following() && c.anchor != l.anchors[tc.target] {
			t.Fatalf("paused key %v: anchor %+v", tc.k, c.anchor)
		}
	}
	c := longChat()
	c.scroll(keyFirst, 40, 9)
	c.scroll(keyUp, 40, 9)
	if c.anchor != (chatAnchor{}) {
		t.Fatal("up underflow")
	}
	for range 20 {
		c.scroll(keyDown, 40, 9)
	}
	if !c.following() {
		t.Fatal("down did not reach end")
	}
	c.scroll(keyFirst, 40, 9)
	for range 20 {
		c.scroll(keyPageDown, 40, 9)
	}
	if !c.following() {
		t.Fatal("page down did not resume")
	}
}

// R-UX5E-XI9H
func TestBrowseChatNoTranscriptScroll(t *testing.T) {
	c := newChatState()
	for _, k := range []key{keyUp, keyDown, keyPageUp, keyPageDown, keyFirst, keyLast} {
		c.scroll(k, 120, 9)
		if !c.following() || c.hasContent() || c.anchor != (chatAnchor{}) {
			t.Fatalf("key %v changed empty state", k)
		}
	}
}

// R-AY3X-9NBN
func TestBrowseRunOpensNodeChat(t *testing.T) {
	original := browserHarnesses
	defer func() { browserHarnesses = original }()
	for h := range original {
		for _, nodeIndex := range []int{0, 2} {
			calls := 0
			browserHarnesses = original
			browserHarnesses[h].list = func(fs.FS, string) ([]session.Session, error) { return []session.Session{{ID: "session"}}, nil }
			browserHarnesses[h].tree = func(fs.FS, string, string) (tree.Tree, error) {
				return tree.Tree{Root: tree.Node{ID: "session"}, Subagents: []tree.Node{{ID: "child", Parent: "session"}, {ID: "grandchild", Parent: "child"}}}, nil
			}
			browserHarnesses[h].chat = func(root fs.FS, home, sid, aid string) (*chat.Transcript, []chat.Entry, error) {
				calls++
				want := "session"
				if nodeIndex == 2 {
					want = "grandchild"
				}
				if home != "/fixture-home" || sid != "session" || aid != want {
					t.Fatalf("chat args %q %q %q", home, sid, aid)
				}
				if _, ok := root.(*browseRoot); !ok {
					t.Fatalf("root %T", root)
				}
				return original[h].chat(root, home, sid, aid)
			}
			keys := make(chan []byte, 1)
			keys <- []byte(strings.Repeat("j", h) + "ll" + strings.Repeat("j", nodeIndex) + "l")
			close(keys)
			terminal := &renderRunTerminal{cols: 120, rows: 12, keys: keys}
			writer := &renderScreenRecorder{}
			if err := Run(context.Background(), Config{Home: "/fixture-home", Root: browseChatHarnessFixture(), Terminal: terminal}, writer); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || len(writer.screens) != 2 {
				t.Fatalf("harness %d node %d calls %d screens %d", h, nodeIndex, calls, len(writer.screens))
			}
			want := "session"
			if nodeIndex == 2 {
				want = "grandchild"
			}
			if !strings.Contains(writer.screens[1], "tokens: in") || !strings.Contains(writer.screens[1], want) {
				t.Fatalf("harness %d node %d screen %q", h, nodeIndex, writer.screens[1])
			}
		}
	}
}

func browseChatHarnessFixture() fstest.MapFS {
	root := fstest.MapFS{}
	put := func(name, data string) { root["fixture-home/"+name] = &fstest.MapFile{Data: []byte(data)} }
	put(".claude/projects/work/session.jsonl", "")
	put(".claude/projects/work/session/subagents/agent-grandchild.jsonl", "")
	put(".codex/sessions/2026/09/24/rollout-2026-09-24T12-00-00-session.jsonl", `{"type":"event_msg","payload":{"item":{"type":"SubAgentActivity","kind":"started","agent_thread_id":"child"}}}`+"\n")
	put(".codex/sessions/2026/09/24/rollout-2026-09-24T12-00-00-child.jsonl", `{"type":"event_msg","payload":{"item":{"type":"SubAgentActivity","kind":"started","agent_thread_id":"grandchild"}}}`+"\n")
	put(".codex/sessions/2026/09/24/rollout-2026-09-24T12-00-00-grandchild.jsonl", "")
	put(".grok/sessions/x/session/summary.json", "{}")
	put(".grok/sessions/x/session/updates.jsonl", "")
	put(".grok/sessions/x/session/subagents/grandchild/meta.json", `{"child_session_id":"grandchild"}`)
	put(".grok/sessions/x/grandchild/updates.jsonl", "")
	return root
}
