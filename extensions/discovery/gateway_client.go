package discovery

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/xraph/forge/errors"
)

// gatewayRequestTimeout bounds each register or deregister call to the gateway.
const gatewayRequestTimeout = 10 * time.Second

// gatewayClient returns the HTTP client for FARP gateway requests, building it
// from config on first use. An invalid TLS configuration is returned as an
// error rather than falling back to an unauthenticated client.
func (e *Extension) gatewayClient() (*http.Client, error) {
	if err := e.config.FARP.checkGatewayTransport(); err != nil {
		return nil, err
	}

	e.gatewayClientMu.Lock()
	defer e.gatewayClientMu.Unlock()

	if e.gatewayHTTP != nil {
		return e.gatewayHTTP, nil
	}

	tlsCfg, err := e.config.FARP.GatewayTLS.build()
	if err != nil {
		return nil, fmt.Errorf("farp gateway tls: %w", err)
	}

	client := &http.Client{Timeout: gatewayRequestTimeout}

	if tlsCfg != nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = tlsCfg
		client.Transport = transport
	}

	e.gatewayHTTP = client

	return client, nil
}

// ErrInsecureGatewayToken is returned when a gateway token would be sent in
// clear text to a host other than this machine.
var ErrInsecureGatewayToken = errors.New(
	"farp gateway token would be sent over plain http to a non-loopback host; " +
		"use an https gateway_url or set allow_insecure_gateway")

// checkGatewayTransport refuses to send the bearer token over plain http to
// anything but loopback, where it would cross the network in clear text,
// unless AllowInsecureGateway is set.
func (c *FARPConfig) checkGatewayTransport() error {
	if c.GatewayToken == "" || c.AllowInsecureGateway {
		return nil
	}

	u, err := url.Parse(c.GatewayURL)
	if err != nil {
		return fmt.Errorf("farp gateway url: %w", err)
	}

	if !strings.EqualFold(u.Scheme, "http") || isLoopbackHost(u.Hostname()) {
		return nil
	}

	return fmt.Errorf("%w (gateway %s)", ErrInsecureGatewayToken, u.Host)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

// authorizeGatewayRequest attaches the configured bearer token, if any.
func (e *Extension) authorizeGatewayRequest(req *http.Request) {
	if e.config.FARP.GatewayToken != "" {
		req.Header.Set("Authorization", "Bearer "+e.config.FARP.GatewayToken)
	}
}

// registeredInstance returns the instance registered at startup, falling back
// to a freshly built one when startup never got that far.
func (e *Extension) registeredInstance() *ServiceInstance {
	e.mu.RLock()
	instance := e.serviceInstance
	e.mu.RUnlock()

	if instance != nil {
		return instance
	}

	return e.createServiceInstance()
}

// build returns nil when no TLS option is set, so the default transport and
// system roots are used.
func (c GatewayTLSConfig) build() (*tls.Config, error) {
	if c.CAFile == "" && c.CertFile == "" && c.KeyFile == "" && c.ServerName == "" {
		return nil, nil //nolint:nilnil // nil config means use the default transport
	}

	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: c.ServerName,
	}

	if c.CAFile != "" {
		pem, err := os.ReadFile(c.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read ca file: %w", err)
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("ca file %s contains no PEM certificates", c.CAFile)
		}

		cfg.RootCAs = pool
	}

	if (c.CertFile == "") != (c.KeyFile == "") {
		return nil, errors.New("cert_file and key_file must be set together")
	}

	if c.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client certificate: %w", err)
		}

		cfg.Certificates = []tls.Certificate{cert}
	}

	return cfg, nil
}
