package browse

import (
	"reflect"
	"testing"
)

func menuFixture(ids ...string) *menuState {
	m := newMenu()
	rows := make([]menuRow, len(ids))
	for i, id := range ids {
		rows[i] = menuRow{line: id, key: menuKey{id: id}}
	}
	m.replace(nil, rows, nil)
	return m
}
func menuIDs(m *menuState) []string {
	ids := make([]string, len(m.rows))
	for i, row := range m.rows {
		ids[i] = row.key.id
	}
	return ids
}

// R-DE5A-G7A6
func TestMenuRowKeys(t *testing.T) {
	m := menuFixture("same", "other", "same", "same")
	want := []menuKey{{"same", 0}, {"other", 0}, {"same", 1}, {"same", 2}}
	if !reflect.DeepEqual(rowKeys(m.rows), want) {
		t.Fatalf("keys: %+v", rowKeys(m.rows))
	}
	m.topLines = []string{"header"}
	m.bottomLines = []string{"", "key"}
	if !reflect.DeepEqual(menuIDs(m), []string{"same", "other", "same", "same"}) {
		t.Fatal("non-menu rows added")
	}
}

// R-DFD6-TZ0V R-DGL3-7QRK
func TestMenuMovement(t *testing.T) {
	m := menuFixture("a", "b", "c")
	for _, step := range []struct {
		k    key
		want int
	}{{keyDown, 1}, {keyDown, 2}, {keyDown, 2}, {keyUp, 1}, {keyUp, 0}, {keyUp, 0}, {keyPageDown, 2}, {keyPageUp, 0}, {keyLast, 2}, {keyFirst, 0}} {
		m.move(step.k, 2)
		if m.highlighted != step.want {
			t.Fatalf("key %v: %d", step.k, m.highlighted)
		}
	}
	ids := make([]string, 50)
	for i := range ids {
		ids[i] = string(rune('a' + i))
	}
	m = menuFixture(ids...)
	m.highlighted = 19
	m.move(keyPageDown, menuRoom(1, 40))
	if m.highlighted != 49 {
		t.Fatalf("page: %d", m.highlighted)
	}
}

// R-DP4D-W4YF R-DQCA-9WP4 R-W5TN-R0VG
func TestMenuRefreshHighlight(t *testing.T) {
	cases := []struct {
		old       []string
		highlight int
		next      []string
		want      int
	}{
		{[]string{"a", "b", "c"}, 1, []string{"z", "c", "b", "a"}, 2},
		{[]string{"a", "b", "c"}, 1, []string{"c", "a"}, 0},
		{[]string{"a", "x", "b", "c"}, 1, []string{"c", "b"}, 0},
		{[]string{"a", "b", "c"}, 2, []string{"a", "new"}, 1},
		{[]string{"a"}, 0, nil, -1},
		{nil, -1, []string{"a"}, 0},
		{[]string{"a", "a", "b"}, 1, []string{"a", "b", "a"}, 2},
	}
	for _, c := range cases {
		m := menuFixture(c.old...)
		m.highlighted = c.highlight
		n := menuFixture(c.next...)
		m.replace(nil, n.rows, nil)
		if m.highlighted != c.want {
			t.Errorf("%v => %v: %d want %d", c.old, c.next, m.highlighted, c.want)
		}
	}
}

// R-UWJ7-Z7RK
func TestBackMissingItem(t *testing.T) {
	for _, c := range []struct {
		old, next []string
		opened    menuKey
		want      int
	}{
		{[]string{"a", "x", "b"}, []string{"b"}, menuKey{"x", 0}, 0},
		{[]string{"a", "y", "x"}, []string{"a", "b"}, menuKey{"x", 0}, 0},
		{[]string{"x", "b"}, nil, menuKey{"x", 0}, -1},
		{[]string{"a", "y", "x"}, []string{"y", "a"}, menuKey{"x", 0}, 0},
	} {
		previous := menuFixture(c.old...)
		m := menuFixture(c.next...)
		m.restore(c.opened, rowKeys(previous.rows))
		if m.highlighted != c.want {
			t.Errorf("%v => %v: %d", c.old, c.next, m.highlighted)
		}
	}
}

// R-DRK6-NOFT R-PLSB-ESVT R-O4FT-OCJ8
func TestMenuBodyRoomAndHighlight(t *testing.T) {
	m := menuFixture("Claude Code", "Codex", "Grok")
	for _, c := range []struct {
		h, top int
		lines  []string
	}{{0, 0, []string{"Claude Code", "Codex"}}, {1, 0, []string{"Claude Code", "Codex"}}, {2, 1, []string{"Codex", "Grok"}}, {1, 1, []string{"Codex", "Grok"}}} {
		m.highlighted = c.h
		body := m.body(0, 5)
		if m.top != c.top {
			t.Errorf("top %d", m.top)
		}
		for i, line := range body {
			if line.text != c.lines[i] || line.highlighted != (i+m.top == c.h) {
				t.Fatalf("body %+v", body)
			}
		}
	}
	m = menuFixture("root", "child")
	m.tree = true
	m.bottomLines = []string{"", "key"}
	for _, c := range []struct {
		height int
		want   []string
	}{{5, []string{"root", "key"}}, {6, []string{"root", "", "key"}}} {
		body := m.body(2, c.height)
		texts := []string{}
		for i, line := range body {
			texts = append(texts, line.text)
			if line.highlighted != (i == 0) {
				t.Fatal("non-menu highlight")
			}
		}
		if !reflect.DeepEqual(texts, c.want) {
			t.Fatalf("height %d: %v", c.height, texts)
		}
	}
	m = menuFixture()
	m.topLines = []string{"header", "no live sessions"}
	body := m.body(1, 5)
	if len(body) != 2 || body[0].text != "header" || body[1].text != "no live sessions" || body[0].highlighted || body[1].highlighted {
		t.Fatalf("empty %+v", body)
	}
	m = newMenu()
	m.failure = "failure"
	body = m.body(2, 5)
	if len(body) != 1 || body[0].highlighted {
		t.Fatalf("failure %+v", body)
	}
	// With no highlight, the remembered top only clamps to the remaining rows.
	m = menuFixture("a", "b", "c", "d")
	m.highlighted = -1
	m.top = 3
	m.body(0, 5)
	if m.top != 2 {
		t.Fatalf("unhighlighted top %d", m.top)
	}
}

// R-DHSZ-LII9
func TestEmptyMenuKeys(t *testing.T) {
	for _, content := range []bool{false, true} {
		b := newBrowserState(Config{}, nil)
		b.level = 1
		b.menus[1] = newMenu()
		b.menus[1].content = content
		b.menus[1].failure = "failure"
		before := *b.menus[1]
		for _, k := range []key{keyUp, keyDown, keyPageUp, keyPageDown, keyFirst, keyLast, keyOpen} {
			if b.apply(k, 120, 40) || !reflect.DeepEqual(*b.menus[1], before) || b.level != 1 {
				t.Fatalf("empty key %v changed state", k)
			}
		}
		b.menus[0] = menuFixture("Claude Code")
		b.openedKeys[0] = b.menus[0].rows[0].key
		if !b.apply(keyBack, 120, 40) || b.level != 0 {
			t.Fatal("empty back")
		}
	}
}
