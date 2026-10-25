// Package inproc is an in-memory gRPC transport. The built-in agent of the
// hub uses it to talk to the hub through the same contract as remote agents,
// without opening a network port.
package inproc

import (
	"context"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const bufferSize = 1 << 20

// Transport connects gRPC clients and a gRPC server in the same process.
type Transport struct {
	lis *bufconn.Listener
}

// New returns a Transport ready to use.
func New() *Transport {
	return &Transport{lis: bufconn.Listen(bufferSize)}
}

// Listener is the listener to pass to grpc.Server.Serve.
func (t *Transport) Listener() net.Listener {
	return t.lis
}

// Dial returns a client connection to the server serving on Listener. The
// connection is in-memory, so no transport security is needed.
func (t *Transport) Dial() (*grpc.ClientConn, error) {
	return grpc.NewClient("passthrough:///inproc",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return t.lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
}
