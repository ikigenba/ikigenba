package apps_test

import (
	"context"
	"io"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
)

type installCloudClient struct {
	get         func(context.Context, string) (io.ReadCloser, error)
	readSecrets func(context.Context, string) (map[string]string, error)
}

func (*installCloudClient) PutObject(context.Context, string, io.Reader) error {
	panic("unexpected PutObject")
}

func (*installCloudClient) ListObjects(context.Context, string) ([]cloud.Object, error) {
	panic("unexpected ListObjects")
}

func (client *installCloudClient) ReadSecrets(ctx context.Context, parameter string) (map[string]string, error) {
	if client.readSecrets == nil {
		panic("unexpected ReadSecrets")
	}
	return client.readSecrets(ctx, parameter)
}

func installStoreAt(t *testing.T, root string, values map[string]string) config.Store {
	t.Helper()
	store := config.Store{Root: root}
	for key, value := range values {
		if err := store.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func (client *installCloudClient) GetObject(ctx context.Context, uri string) (io.ReadCloser, error) {
	return client.get(ctx, uri)
}
