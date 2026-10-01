package main

import (
	"context"
	"github.com/chainreactors/cyber/pkg/web"
	"net"
	"net/http"
)

func serveManagedHTTP(ctx context.Context, server *http.Server, listener net.Listener, closeResources func(context.Context) error) error {
	return web.ServeHTTP(ctx, server, listener, closeResources)
}
