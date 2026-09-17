package account

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

// PropertiesParameter is the secure parameter containing account properties.
const PropertiesParameter = "/ikigenba/account"

// Properties controls the resources and backup policy for an account.
type Properties struct {
	Domain                    string `json:"domain"`
	BackupBucket              string `json:"backup_bucket"`
	LaunchTemplateID          string `json:"launch_template_id"`
	PermissionsBoundaryARN    string `json:"permissions_boundary_arn"`
	Region                    string `json:"region"`
	DeleteSecretsOnDestroy    bool   `json:"delete_secrets_on_destroy"`
	DeleteBackupsOnDestroy    bool   `json:"delete_backups_on_destroy"`
	BackupHostFilesSeconds    int    `json:"backup_host_files_seconds"`
	BackupServiceFilesSeconds int    `json:"backup_service_files_seconds"`
	BackupServiceDBSeconds    int    `json:"backup_service_db_seconds"`
	BackupServiceWALSeconds   int    `json:"backup_service_wal_seconds"`
}

// Account is an opened account and its regional cloud clients.
type Account struct {
	Profile    string
	Properties Properties
	Clients    cloud.Clients
}

// Space is the account-facing view of a deployment instance.
type Space struct {
	Domain  string
	ID      string
	State   cloud.InstanceState
	Address string
}

// NoSpaceError reports that a domain has no deployment instance.
type NoSpaceError struct {
	Domain string
}

// Error returns the user-facing missing-space diagnostic.
func (e *NoSpaceError) Error() string { return fmt.Sprintf("no space at '%s'", e.Domain) }

// NoZoneError reports that no hosted zone contains a domain.
type NoZoneError struct {
	Domain string
}

// Error returns the user-facing missing-zone diagnostic.
func (e *NoZoneError) Error() string { return fmt.Sprintf("no hosted zone for '%s'", e.Domain) }

// Open loads account properties and opens clients in the configured region.
func Open(ctx context.Context, deps seam.Deps, profile string) (*Account, error) {
	bootstrap, err := deps.Cloud(ctx, profile, "")
	if err != nil {
		return nil, err
	}

	value, err := bootstrap.SSM.GetParameter(ctx, PropertiesParameter)
	if err != nil {
		return nil, err
	}

	properties, err := decodeProperties(value)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", PropertiesParameter, err)
	}

	clients, err := deps.Cloud(ctx, profile, properties.Region)
	if err != nil {
		return nil, err
	}

	return &Account{Profile: profile, Properties: properties, Clients: clients}, nil
}

func decodeProperties(value string) (Properties, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &fields); err != nil {
		return Properties{}, err
	}
	if fields == nil {
		return Properties{}, fmt.Errorf("properties must be a JSON object")
	}

	var properties Properties
	required := []struct {
		name string
		dest any
	}{
		{"domain", &properties.Domain},
		{"backup_bucket", &properties.BackupBucket},
		{"launch_template_id", &properties.LaunchTemplateID},
		{"permissions_boundary_arn", &properties.PermissionsBoundaryARN},
		{"region", &properties.Region},
		{"delete_secrets_on_destroy", &properties.DeleteSecretsOnDestroy},
		{"delete_backups_on_destroy", &properties.DeleteBackupsOnDestroy},
		{"backup_host_files_seconds", &properties.BackupHostFilesSeconds},
		{"backup_service_files_seconds", &properties.BackupServiceFilesSeconds},
		{"backup_service_db_seconds", &properties.BackupServiceDBSeconds},
		{"backup_service_wal_seconds", &properties.BackupServiceWALSeconds},
	}
	for _, field := range required {
		raw, ok := fields[field.name]
		if !ok {
			return Properties{}, fmt.Errorf("missing %q", field.name)
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return Properties{}, fmt.Errorf("%s: null has the wrong type", field.name)
		}
		if err := json.Unmarshal(raw, field.dest); err != nil {
			return Properties{}, fmt.Errorf("%s: %w", field.name, err)
		}
	}

	periods := []int{
		properties.BackupHostFilesSeconds,
		properties.BackupServiceFilesSeconds,
		properties.BackupServiceDBSeconds,
		properties.BackupServiceWALSeconds,
	}
	for _, period := range periods {
		if period < 0 {
			return Properties{}, fmt.Errorf("backup periods must not be negative")
		}
	}

	return properties, nil
}
