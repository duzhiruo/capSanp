package httpapi

import (
	"context"

	"capsnap/internal/agent"
	"capsnap/internal/reqctx"
)

func NewRequestID() string {
	return agent.NewID("req")
}

func WithRequestID(ctx context.Context, id string) context.Context {
	return reqctx.With(ctx, id)
}

func GetRequestID(ctx context.Context) string {
	return reqctx.Get(ctx)
}
