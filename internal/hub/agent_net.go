package hub

import (
	"context"
	"errors"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/CodexCircle/Regulus/internal/auth"
	"github.com/CodexCircle/Regulus/internal/fleet"
	agentv1 "github.com/CodexCircle/Regulus/internal/gen/regulus/agent/v1"
	"github.com/CodexCircle/Regulus/internal/pki"
)

// identityKey carries the verified identity of a remote agent in the context
// of its stream. The in-process connection of the built-in agent has none.
type identityKey struct{}

func identityFrom(ctx context.Context) (pki.Identity, bool) {
	id, ok := ctx.Value(identityKey{}).(pki.Identity)
	return id, ok
}

// peerIdentity returns the verified client certificate of the caller.
func peerIdentity(ctx context.Context) (pki.Identity, bool) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return pki.Identity{}, false
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return pki.Identity{}, false
	}
	return pki.ClientIdentity(info.State)
}

func peerHost(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return ""
	}
	host, _, err := net.SplitHostPort(p.Addr.String())
	if err != nil {
		return p.Addr.String()
	}
	return host
}

// wrappedStream replaces the context of a server stream.
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w wrappedStream) Context() context.Context { return w.ctx }

// requireAgent is the stream interceptor of the network listener: a stream
// needs the certificate of an agent that was enrolled and not revoked.
func (h *Hub) requireAgent(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx := ss.Context()
	id, ok := peerIdentity(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "a client certificate is required")
	}
	if err := h.fleet.Authorize(ctx, id.AgentID, id.Serial); err != nil {
		switch {
		case errors.Is(err, fleet.ErrNotFound), errors.Is(err, fleet.ErrRevoked):
			h.log.Warn("refusing agent", "agent", id.AgentID, "err", err)
			return status.Error(codes.PermissionDenied, "this agent is not enrolled or was revoked")
		default:
			h.log.Error("checking agent", "agent", id.AgentID, "err", err)
			return status.Error(codes.Internal, "cannot check the agent")
		}
	}
	return handler(srv, wrappedStream{ss, context.WithValue(ctx, identityKey{}, id)})
}

// enrollServer implements the one call that works without a certificate.
type enrollServer struct {
	agentv1.UnimplementedEnrollmentServiceServer
	hub *Hub
}

func (e enrollServer) Enroll(ctx context.Context, req *agentv1.EnrollRequest) (*agentv1.EnrollResponse, error) {
	remote := peerHost(ctx)
	ctx = auth.WithClient(ctx, auth.Client{Remote: remote})
	out, err := e.hub.fleet.Enroll(ctx, remote, req.GetToken(), req.GetCsrDer(), req.GetSystem().GetHostname())
	var reqErr *fleet.RequestError
	switch {
	case err == nil:
		e.hub.log.Info("agent enrolled", "agent", out.AgentID, "host", req.GetSystem().GetHostname(), "from", remote)
		return &agentv1.EnrollResponse{
			AgentId:          out.AgentID,
			CertificateDer:   out.CertificateDER,
			CaCertificateDer: out.CACertificateDER,
		}, nil
	case errors.Is(err, fleet.ErrInvalidToken):
		return nil, status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, fleet.ErrRateLimited):
		return nil, status.Error(codes.ResourceExhausted, err.Error())
	case errors.As(err, &reqErr):
		return nil, status.Error(codes.InvalidArgument, err.Error())
	default:
		e.hub.log.Error("enrolling an agent", "err", err)
		return nil, status.Error(codes.Internal, "cannot enroll the agent")
	}
}
