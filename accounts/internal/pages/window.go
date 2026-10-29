package pages

import "github.com/ikigenba/ikigenba/appkit/paging"

type position struct{ kind, key string }

func decode(name, value string) (position, bool) {
	if value == "" {
		return position{}, true
	}
	var key string
	if (paging.Token{Tool: name}).Decode(value, &key) == nil {
		return position{kind: "after", key: key}, true
	}
	if (paging.Token{Tool: name + "-before"}).Decode(value, &key) == nil {
		return position{kind: "before", key: key}, true
	}
	if value == "last" {
		return position{kind: "last"}, true
	}
	return position{}, false
}

type window struct {
	start, end int
	prev, next string
	underflow  bool
}

func listWindow[T any](entries []T, name, value string, p position, key func(T) string) window {
	n := len(entries)
	start, end := 0, n
	switch p.kind {
	case "after":
		for start < n && key(entries[start]) <= p.key {
			start++
		}
	case "before":
		end = 0
		for end < n && key(entries[end]) < p.key {
			end++
		}
	case "last":
	}
	under := (p.kind == "before" && end <= paging.DefaultLimit) || (p.kind == "last" && n <= paging.DefaultLimit)
	if p.kind == "before" || p.kind == "last" {
		start = max(0, end-paging.DefaultLimit)
	} else {
		end = min(end, start+paging.DefaultLimit)
	}
	w := window{start: start, end: end, underflow: under}
	if start < end {
		if value != "" && !under {
			w.prev = (paging.Token{Tool: name + "-before"}).Encode(key(entries[start]))
		}
		if end < n {
			w.next = (paging.Token{Tool: name}).Encode(key(entries[end-1]))
		}
	}
	return w
}
