package transport

import (
	"context"
	"errors"
	"net/url"
	"time"

	"google.golang.org/grpc"
	_ "google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/resolver"
)

// GRPC binds generated protobuf clients to a service name with explicit credentials.
func (s *Services) GRPC(service string, creds credentials.TransportCredentials, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	if creds == nil {
		return nil, errors.New("conduit: gRPC transport credentials are required")
	}

	protocol := "grpc"
	if creds.Info().SecurityProtocol == "tls" {
		protocol = "grpcs"
	}

	builder := &serviceBuilder{services: s, service: service, protocol: protocol}
	options := []grpc.DialOption{grpc.WithTransportCredentials(creds), grpc.WithResolvers(builder), grpc.WithDefaultServiceConfig(`{"loadBalancingConfig":[{"round_robin":{}}]}`)}
	options = append(options, opts...)

	return grpc.NewClient("forge:///"+url.PathEscape(service), options...)
}

type serviceBuilder struct {
	services *Services
	service  string
	protocol string
}

func (b *serviceBuilder) Scheme() string { return "forge" }
func (b *serviceBuilder) Build(_ resolver.Target, conn resolver.ClientConn, _ resolver.BuildOptions) (resolver.Resolver, error) {
	ctx, cancel := context.WithCancel(context.Background())

	r := &serviceResolver{builder: b, conn: conn, cancel: cancel, wake: make(chan struct{}, 1)}
	go r.run(ctx)

	return r, nil
}

type serviceResolver struct {
	builder *serviceBuilder
	conn    resolver.ClientConn
	cancel  context.CancelFunc
	wake    chan struct{}
}

func (r *serviceResolver) ResolveNow(resolver.ResolveNowOptions) {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}
func (r *serviceResolver) Close() { r.cancel() }
func (r *serviceResolver) run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		resolveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		endpoints, err := r.builder.services.endpoints(resolveCtx, r.builder.service, r.builder.protocol)

		cancel()

		if err != nil {
			_ = r.conn.UpdateState(resolver.State{Addresses: []resolver.Address{}})
			r.conn.ReportError(err)
		} else {
			addresses := make([]resolver.Address, 0, len(endpoints))
			for _, endpoint := range endpoints {
				parsed, err := url.Parse(endpoint.URL)
				if err == nil && parsed.Host != "" && parsed.User == nil && parsed.Scheme == r.builder.protocol {
					addresses = append(addresses, resolver.Address{Addr: parsed.Host, ServerName: parsed.Hostname()})
				}
			}

			if len(addresses) == 0 {
				_ = r.conn.UpdateState(resolver.State{Addresses: []resolver.Address{}})
				r.conn.ReportError(errors.New("conduit: no valid advertised gRPC endpoint"))
			} else {
				_ = r.conn.UpdateState(resolver.State{Addresses: addresses})
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-r.wake:
		case <-ticker.C:
		}
	}
}
