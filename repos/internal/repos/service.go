package repos

import "eventplane/outbox"

// Service is the single domain object assembled by the composition root.
// The v2 behavior is introduced by later phases.
type Service struct {
	store    *Store
	producer *outbox.Outbox
}

func NewService(store *Store) *Service { return &Service{store: store} }

// SetProducer injects the chassis outbox after handlers have been assembled.
func (s *Service) SetProducer(producer *outbox.Outbox) { s.producer = producer }
