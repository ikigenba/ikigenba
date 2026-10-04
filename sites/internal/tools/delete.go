package tools

import (
	"context"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

func (cfg Config) delete(ctx context.Context, u identity.Caller, a DeleteArgs) (Deleted, error) {
	s, err := cfg.find(ctx, u.UserID, a.Name)
	if err != nil {
		return Deleted{}, err
	}
	apex, err := cfg.Store.Delete(ctx, s.ID)
	if err != nil {
		return Deleted{}, catalogError(err)
	}
	_ = cfg.Cache.Remove(s.ID)
	cfg.emit(ctx, "site.deleted", telemetry.Attrs{"site": s.ID})
	if apex {
		cfg.emit(ctx, "site.apex", telemetry.Attrs{"site": ""})
	}
	return Deleted{Deleted: true, ID: s.ID}, nil
}
