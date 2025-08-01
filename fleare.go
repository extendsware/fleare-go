package fleare

import "context"

func CreateClient(opts *Options) *Client {
	ctx := context.Background()
	return NewClient(ctx, opts)
}

func CreateClientWithContext(ctx context.Context, opts *Options) *Client {
	return NewClient(ctx, opts)
}

type CommandInterface interface {
	Set(ctx context.Context, key string, value interface{}) (interface{}, error)
	Get(ctx context.Context, key string, path ...string) (interface{}, error)
}
