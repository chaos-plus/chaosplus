package natsclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/infra/runnergateway"
	"github.com/chaos-plus/chaosplus/internal/core/extension/secretx"
	"github.com/nats-io/nats.go"
)

type Config struct {
	URL       string        `mapstructure:"url" description:"NATS server URL" default:"nats://127.0.0.1:4222"`
	Token     string        `mapstructure:"token" description:"NATS authentication token" default:""`
	TokenFile string        `mapstructure:"token_file" description:"file containing the NATS token; mutually exclusive with token" default:""`
	TLSCA     string        `mapstructure:"tls_ca" description:"PEM CA file for NATS TLS" default:""`
	TLSCert   string        `mapstructure:"tls_cert" description:"PEM client certificate for NATS mTLS" default:""`
	TLSKey    string        `mapstructure:"tls_key" description:"PEM client key for NATS mTLS" default:""`
	Timeout   time.Duration `mapstructure:"timeout" description:"NATS connection timeout" default:"5s"`
}

type Client struct {
	primary *nats.Conn
	bridge  *nats.Conn
	gateway *gateway.Gateway
	cancel  context.CancelFunc
}

func Open(config Config) (*Client, error) {
	if strings.TrimSpace(config.URL) == "" {
		return nil, errors.New("NATS URL is required")
	}
	if config.Timeout <= 0 || config.Timeout > time.Minute {
		return nil, errors.New("NATS timeout must be between zero and one minute")
	}
	if (config.TLSCert == "") != (config.TLSKey == "") {
		return nil, errors.New("NATS TLS certificate and key must be configured together")
	}
	token, err := secretx.Resolve("nats.token", config.Token, config.TokenFile, 64<<10)
	if err != nil {
		return nil, err
	}
	options := []nats.Option{nats.Timeout(config.Timeout), nats.NoEcho()}
	if token != "" {
		options = append(options, nats.Token(token))
	}
	if config.TLSCA != "" {
		options = append(options, nats.RootCAs(config.TLSCA))
	}
	if config.TLSCert != "" {
		options = append(options, nats.ClientCert(config.TLSCert, config.TLSKey))
	}
	primary, err := nats.Connect(config.URL, append(options, nats.Name("resource-control"))...)
	if err != nil {
		return nil, fmt.Errorf("connect NATS: %w", err)
	}
	bridgeOptions := []nats.Option{nats.Timeout(config.Timeout)}
	if token != "" {
		bridgeOptions = append(bridgeOptions, nats.Token(token))
	}
	if config.TLSCA != "" {
		bridgeOptions = append(bridgeOptions, nats.RootCAs(config.TLSCA))
	}
	if config.TLSCert != "" {
		bridgeOptions = append(bridgeOptions, nats.ClientCert(config.TLSCert, config.TLSKey))
	}
	bridge, err := nats.Connect(config.URL, append(bridgeOptions, nats.Name("runner-bridge"))...)
	if err != nil {
		primary.Close()
		return nil, fmt.Errorf("connect NATS runner bridge: %w", err)
	}
	return &Client{primary: primary, bridge: bridge, gateway: gateway.New(primary)}, nil
}

func (c *Client) Primary() *nats.Conn       { return c.primary }
func (c *Client) Bridge() *nats.Conn        { return c.bridge }
func (c *Client) Gateway() *gateway.Gateway { return c.gateway }

func (c *Client) Start(ctx context.Context) error {
	if c.cancel != nil {
		return errors.New("NATS client already started")
	}
	workerContext, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	return c.gateway.Start(workerContext)
}

func (c *Client) Stop(ctx context.Context) error {
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	var failures []error
	for name, connection := range map[string]*nats.Conn{"primary": c.primary, "bridge": c.bridge} {
		if connection == nil {
			continue
		}
		done := make(chan error, 1)
		go func() { done <- connection.Drain() }()
		select {
		case err := <-done:
			if err != nil {
				failures = append(failures, fmt.Errorf("drain NATS %s: %w", name, err))
			}
		case <-ctx.Done():
			connection.Close()
			failures = append(failures, fmt.Errorf("drain NATS %s: %w", name, ctx.Err()))
		}
	}
	return errors.Join(failures...)
}
