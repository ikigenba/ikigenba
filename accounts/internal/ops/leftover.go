package ops

import (
	"context"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
)

func (e *Engine) checkLeftovers(ctx context.Context) error {
	for after := int64(0); ; {
		hosts, err := e.cfg.Store.DeletedHosts(ctx, after, store.BatchSize)
		if err != nil {
			return err
		}
		for _, host := range hosts {
			if host.EIP != "" {
				if err := e.deleteLeftoverRecord(ctx, host); err != nil {
					return err
				}
			}
			if err := e.cfg.Store.SettleDeleted(ctx, host.Name); err != nil {
				return err
			}
		}
		if len(hosts) < store.BatchSize {
			return nil
		}
		after = hosts[len(hosts)-1].Seq
	}
}

func (e *Engine) deleteLeftoverRecord(ctx context.Context, host store.Host) error {
	name, zone := e.recordName(host.Name), e.cfg.Hosts.ZoneID
	records, err := e.cfg.Cloud.FindRecords(ctx, zone, name)
	if err != nil {
		return err
	}
	for _, record := range records {
		if pointsAt(record, host.EIP) {
			_, err := e.cfg.Cloud.ChangeRecord(ctx, zone, cloud.RecordChange{
				Action: cloud.RecordDelete, Name: name, IP: host.EIP, TTL: record.TTL,
			})
			return err
		}
	}
	return nil
}
