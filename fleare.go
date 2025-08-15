package fleare

import "context"

func CreateClient(opts *Options) *Client {
	ctx := context.Background()
	return NewClient(ctx, opts)
}

func CreateClientWithContext(ctx context.Context, opts *Options) *Client {
	return NewClient(ctx, opts)
}
