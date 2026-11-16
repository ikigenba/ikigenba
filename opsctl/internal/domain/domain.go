// Package domain stores and validates the host's custom domains.
package domain

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
)

// Key holds the custom-domain list in the host configuration.
const Key = "host.domains"

// Domain names either a primary domain or a redirect to Primary.
type Domain struct {
	Name    string
	Primary string
}

// ErrMalformed identifies an invalid custom-domain list.
var ErrMalformed = errors.New("host.domains malformed")

// ValidName reports whether name is a lower-case ASCII host name.
func ValidName(name string) bool {
	if len(name) == 0 || len(name) > 253 {
		return false
	}
	labels := strings.Split(name, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := range len(label) {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	for _, c := range labels[len(labels)-1] {
		if c < '0' || c > '9' {
			return true
		}
	}
	return false
}

// List reads the validated list in ascending name order without writing it.
func List(store config.Store) ([]Domain, error) {
	value, err := store.Get(Key)
	if errors.Is(err, config.ErrNotSet) {
		return []Domain{}, nil
	}
	if err != nil {
		return nil, err
	}
	if value == "" {
		return []Domain{}, nil
	}
	entries := strings.Split(value, ",")
	list := make([]Domain, len(entries))
	primaries := make(map[string]bool, len(entries))
	for i, entry := range entries {
		name, primary, redirect := strings.Cut(entry, ":")
		list[i] = Domain{Name: name, Primary: primary}
		if !redirect {
			primaries[name] = true
		}
	}
	seen := make(map[string]bool, len(entries))
	for i, d := range list {
		_, _, redirect := strings.Cut(entries[i], ":")
		if !ValidName(d.Name) || seen[d.Name] || redirect && (!ValidName(d.Primary) || !primaries[d.Primary]) {
			return nil, malformed(entries[i])
		}
		seen[d.Name] = true
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

// Save validates and stores the list canonically, deleting an empty list.
func Save(store config.Store, list []Domain) error {
	primaries := make(map[string]bool, len(list))
	for _, d := range list {
		if d.Primary == "" {
			primaries[d.Name] = true
		}
	}
	seen := make(map[string]bool, len(list))
	for _, d := range list {
		if !ValidName(d.Name) || seen[d.Name] || d.Primary != "" && (!ValidName(d.Primary) || !primaries[d.Primary]) {
			return malformed(render(d))
		}
		seen[d.Name] = true
	}
	if len(list) == 0 {
		return store.Del(Key)
	}
	ordered := append([]Domain(nil), list...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	entries := make([]string, len(ordered))
	for i, d := range ordered {
		entries[i] = render(d)
	}
	return store.Set(Key, strings.Join(entries, ","))
}

func render(d Domain) string {
	if d.Primary == "" {
		return d.Name
	}
	return d.Name + ":" + d.Primary
}

func malformed(entry string) error { return fmt.Errorf("%w: %q", ErrMalformed, entry) }
