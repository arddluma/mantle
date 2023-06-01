package client

import (
	"context"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/mantlenetworkio/mantle/mt-node/client"
	"github.com/mantlenetworkio/mantle/mt-node/sources"
)

// DialRollupClientWithTimeout attempts to dial the RPC provider using the provided
// URL. If the dial doesn't complete within defaultDialTimeout seconds, this
// method will return an error.
func DialRollupClientWithTimeout(ctx context.Context, url string, timeout time.Duration) (*sources.RollupClient, error) {
	ctxt, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rpcCl, err := rpc.DialContext(ctxt, url)
	if err != nil {
		return nil, err
	}

	return sources.NewRollupClient(client.NewBaseRPCClient(rpcCl)), nil
}
