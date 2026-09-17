// Package cloud defines the provider-neutral cloud boundary.
package cloud

import "context"

// Clients contains cloud service clients.
//
// The service-specific fields are added with the cloud implementation.
type Clients struct{}

// Opener opens cloud clients for a profile and region.
type Opener func(ctx context.Context, profile, region string) (Clients, error)
