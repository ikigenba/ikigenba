package tools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/cache"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/urls"
)

func updateRules(ctx context.Context, a UpdateArgs) error {
	if !store.ValidName(a.Name) {
		return fmt.Errorf(MissingSite, a.Name)
	}
	if a.Visibility == nil && a.Listed == nil && a.Ref == nil && a.Path == nil && a.Domain == nil {
		return errors.New(EmptyUpdate)
	}
	if a.Visibility != nil && *a.Visibility != store.Public && *a.Visibility != store.Private {
		return errors.New(BadVisibility)
	}
	if a.Ref != nil && !cache.ValidRef(*a.Ref) {
		return fmt.Errorf(InvalidRef, *a.Ref)
	}
	if a.Path != nil && *a.Path != "" && !cache.ValidPath(*a.Path) {
		return fmt.Errorf(InvalidPath, *a.Path)
	}
	if a.Domain != nil && *a.Domain != "" {
		if !store.ValidDomain(*a.Domain) {
			return fmt.Errorf(InvalidDomain, *a.Domain)
		}
		base, _ := urls.FromContext(ctx)
		parsed, _ := url.Parse(base)
		if parsed != nil {
			host := strings.Map(func(r rune) rune {
				if r >= 'A' && r <= 'Z' {
					return r + ('a' - 'A')
				}
				return r
			}, parsed.Hostname())
			if strings.HasPrefix(host, "sites.") {
				space := strings.TrimPrefix(host, "sites.")
				_, apex, hasApex := strings.Cut(space, ".")
				if *a.Domain == space || strings.HasSuffix(*a.Domain, "."+space) || hasApex && *a.Domain == apex {
					return fmt.Errorf(SpaceDomain, *a.Domain)
				}
			}
		}
	}
	return nil
}
func (cfg Config) update(ctx context.Context, u identity.Caller, a UpdateArgs) (Site, error) {
	s, err := cfg.find(ctx, u.UserID, a.Name)
	if err != nil {
		if err.Error() == store.Unreachable {
			if rule := updateRules(ctx, a); rule != nil {
				return Site{}, rule
			}
		}
		return Site{}, err
	}
	if rule := updateRules(ctx, a); rule != nil {
		return Site{}, rule
	}
	s, changed, err := cfg.Store.Update(ctx, s.ID, store.Change{Visibility: a.Visibility, Listed: a.Listed, Ref: a.Ref, Path: a.Path, Domain: a.Domain})
	if errors.Is(err, store.ErrNotPublic) {
		return Site{}, errors.New(ApexNotPublic)
	}
	if errors.Is(err, store.ErrDomainNotPublic) {
		return Site{}, errors.New(DomainNotPublic)
	}
	if errors.Is(err, store.ErrDomainTaken) {
		return Site{}, fmt.Errorf(DomainTaken, *a.Domain)
	}
	if err != nil {
		return Site{}, catalogError(err)
	}
	for _, field := range changed {
		value := s.Ref
		switch field {
		case "visibility":
			value = s.Visibility
		case "path":
			value = s.Path
		case "domain":
			value = s.Domain
		case "listed":
			value = strconv.FormatBool(s.Listed)
		}
		cfg.emit(ctx, "site.updated", telemetry.Attrs{"site": s.ID, "field": field, "value": value})
	}
	return siteObject(ctx, s), nil
}
