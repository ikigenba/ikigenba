package store

import (
	"hash/fnv"
	"reflect"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// displayOrder is a link's display position, followed by an occurrence in it.
type displayOrder struct {
	gone            bool
	filed, seen     time.Time
	scraper, source string
	offset          int
	name            string
}

func orderLink(l Link) displayOrder {
	return displayOrder{gone: l.Gone, filed: l.Reading.Filed, seen: l.LastSeen, scraper: l.Scraper, source: l.SourceID}
}
func orderLess(a, b displayOrder) bool {
	if a.gone != b.gone {
		return !a.gone
	}
	if !a.filed.Equal(b.filed) {
		return a.filed.After(b.filed)
	}
	if !a.seen.Equal(b.seen) {
		return a.seen.After(b.seen)
	}
	if a.scraper != b.scraper {
		return a.scraper < b.scraper
	}
	if a.source != b.source {
		return a.source < b.source
	}
	if a.offset != b.offset {
		return a.offset < b.offset
	}
	return a.name < b.name
}

// The deterministic treap supports display winners and name positions without
// sorting or scanning all of a candidate's links after each observation.
type displayTree struct {
	key         displayOrder
	priority    uint64
	size        int
	left, right *displayTree
}

func treeSize(n *displayTree) int {
	if n == nil {
		return 0
	}
	return n.size
}
func treeFix(n *displayTree) { n.size = 1 + treeSize(n.left) + treeSize(n.right) }
func treeJoin(a, b *displayTree) *displayTree {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if a.priority > b.priority {
		a.right = treeJoin(a.right, b)
		treeFix(a)
		return a
	}
	b.left = treeJoin(a, b.left)
	treeFix(b)
	return b
}
func treeSplit(n *displayTree, k displayOrder) (*displayTree, *displayTree) {
	if n == nil {
		return nil, nil
	}
	if orderLess(n.key, k) {
		a, b := treeSplit(n.right, k)
		n.right = a
		treeFix(n)
		return n, b
	}
	a, b := treeSplit(n.left, k)
	n.left = b
	treeFix(n)
	return a, n
}
func treeInsert(n *displayTree, k displayOrder) *displayTree {
	h := fnv.New64a()
	_, _ = h.Write([]byte(k.scraper + "\x00" + k.source + "\x00" + k.name))
	node := &displayTree{key: k, priority: h.Sum64(), size: 1}
	return treePut(n, node)
}
func treePut(n, node *displayTree) *displayTree {
	if n == nil {
		return node
	}
	if node.priority > n.priority {
		node.left, node.right = treeSplit(n, node.key)
		treeFix(node)
		return node
	}
	if orderLess(node.key, n.key) {
		n.left = treePut(n.left, node)
	} else {
		n.right = treePut(n.right, node)
	}
	treeFix(n)
	return n
}
func treeRemove(n *displayTree, k displayOrder) *displayTree {
	if n == nil {
		return nil
	}
	switch {
	case orderLess(k, n.key):
		n.left = treeRemove(n.left, k)
	case orderLess(n.key, k):
		n.right = treeRemove(n.right, k)
	default:
		return treeJoin(n.left, n.right)
	}
	treeFix(n)
	return n
}
func treeFirst(n *displayTree) displayOrder {
	for n.left != nil {
		n = n.left
	}
	return n.key
}
func treeRank(n *displayTree, k displayOrder) int {
	if n == nil {
		return 0
	}
	if orderLess(k, n.key) {
		return treeRank(n.left, k)
	}
	if orderLess(n.key, k) {
		return treeSize(n.left) + 1 + treeRank(n.right, k)
	}
	return treeSize(n.left)
}
func readingNames(l Link) map[string]displayOrder {
	out := map[string]displayOrder{}
	names := append([]string{l.Reading.Name}, l.Reading.Names...)
	for i, name := range names {
		if name == "" {
			continue
		}
		if _, ok := out[name]; ok {
			continue
		}
		k := orderLink(l)
		k.offset = i
		k.name = name
		out[name] = k
	}
	return out
}

type displayFields struct {
	name, street, city, state, zip, county, phone, email, class string
	people                                                      []sources.Person
}

func fieldsReading(r sources.Reading) displayFields {
	return displayFields{r.Name, r.Street, r.City, r.State, r.Zip, r.County, r.Phone, r.Email, r.Class, append([]sources.Person{}, r.People...)}
}
func fieldsCandidate(c Candidate) displayFields {
	return displayFields{c.Name, c.Street, c.City, c.State, c.Zip, c.County, c.Phone, c.Email, c.Class, append([]sources.Person{}, c.People...)}
}

type displayTracker struct {
	records      *records
	id           string
	links, names *displayTree
	mentions     map[string]*displayTree
	cargo        map[string]int
	changed      bool
	fields       displayFields
}

func newDisplayTracker(r *records, id string) *displayTracker {
	d := &displayTracker{records: r, id: id, mentions: map[string]*displayTree{}, cargo: map[string]int{}, fields: fieldsCandidate(r.candidates[id])}
	for _, l := range r.links {
		d.links = treeInsert(d.links, orderLink(l))
		for name, key := range readingNames(l) {
			d.mentions[name] = treeInsert(d.mentions[name], key)
		}
		for _, v := range l.Reading.Cargo {
			d.cargo[v]++
		}
	}
	for _, mentions := range d.mentions {
		d.names = treeInsert(d.names, treeFirst(mentions))
	}
	return d
}
func (d *displayTracker) apply(l Link) {
	source := sources.SourceKey(l.Scraper, l.SourceID)
	old, exists := d.records.links[source]
	oldNames, newNames := readingNames(old), readingNames(l)
	affected := map[string]bool{}
	for name := range oldNames {
		affected[name] = true
	}
	for name := range newNames {
		affected[name] = true
	}
	ranks := map[string]int{}
	for name := range affected {
		if mentions := d.mentions[name]; mentions != nil {
			key := treeFirst(mentions)
			ranks[name] = treeRank(d.names, key)
		}
	}
	for name := range affected {
		if mentions := d.mentions[name]; mentions != nil {
			d.names = treeRemove(d.names, treeFirst(mentions))
		}
	}
	oldCargo := map[string]bool{}
	for _, v := range old.Reading.Cargo {
		oldCargo[v] = d.cargo[v] > 0
	}
	for _, v := range l.Reading.Cargo {
		oldCargo[v] = d.cargo[v] > 0
	}
	if exists {
		d.links = treeRemove(d.links, orderLink(old))
		for name, key := range oldNames {
			d.mentions[name] = treeRemove(d.mentions[name], key)
		}
		for _, v := range old.Reading.Cargo {
			d.cargo[v]--
		}
	}
	d.records.links[source] = l
	d.links = treeInsert(d.links, orderLink(l))
	for name, key := range newNames {
		d.mentions[name] = treeInsert(d.mentions[name], key)
	}
	for _, v := range l.Reading.Cargo {
		d.cargo[v]++
	}
	for name := range affected {
		if mentions := d.mentions[name]; mentions != nil {
			d.names = treeInsert(d.names, treeFirst(mentions))
		}
	}
	for name := range affected {
		rank, had := ranks[name]
		mentions := d.mentions[name]
		if had != (mentions != nil) || had && rank != treeRank(d.names, treeFirst(mentions)) {
			d.changed = true
		}
	}
	for v, had := range oldCargo {
		if had != (d.cargo[v] > 0) {
			d.changed = true
		}
	}
	winner := treeFirst(d.links)
	fields := fieldsReading(d.records.links[sources.SourceKey(winner.scraper, winner.source)].Reading)
	if !reflect.DeepEqual(d.fields, fields) {
		d.changed = true
	}
	d.fields = fields
}
