package rpc

import (
	"context"
	"time"

	"github.com/otpki/otpki-pkg/spfy/pulse"
	"google.golang.org/protobuf/proto"
)

type Client struct {
	backend        pulse.Backend
	defaultTimeout time.Duration
	reliability    pulse.Reliability
}

type ClientOption func(*Client)

func WithDefaultTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) { c.defaultTimeout = timeout }
}

func WithReliability(r pulse.Reliability) ClientOption {
	return func(c *Client) { c.reliability = r }
}

func NewClient(backend pulse.Backend, opts ...ClientOption) *Client {
	c := &Client{backend: backend, defaultTimeout: 5 * time.Second}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) Call(ctx context.Context, method Method, req proto.Message, resp proto.Message, header pulse.Header, opts pulse.RequestOptions) (pulse.Header, error) {
	future, err := c.CallAsync(ctx, method, req, header, opts)
	if err != nil {
		return nil, err
	}
	msg, err := future.Await(ctx)
	if err != nil {
		return nil, err
	}
	if err := ErrorFromMessage(msg); err != nil {
		return nil, err
	}
	if resp != nil {
		if err := proto.Unmarshal(msg.Data, resp); err != nil {
			return nil, err
		}
	}
	return msg.Header.Clone(), nil
}

func (c *Client) CallAsync(ctx context.Context, method Method, req proto.Message, header pulse.Header, opts pulse.RequestOptions) (pulse.Future[pulse.Message], error) {
	if c == nil || c.backend == nil {
		return pulse.Future[pulse.Message]{}, pulse.ErrClosed
	}
	if opts.Timeout == 0 {
		opts.Timeout = c.defaultTimeout
	}
	if opts.Reliability == pulse.Ephemeral {
		opts.Reliability = c.reliability
	}
	data, err := proto.Marshal(req)
	if err != nil {
		return pulse.Future[pulse.Message]{}, err
	}
	return c.backend.RequestAsync(ctx, method.Subject(), pulse.Message{Header: header.Clone(), Data: data}, opts)
}

func (c *Client) Fire(ctx context.Context, method Method, req proto.Message, header pulse.Header, opts pulse.RequestOptions) error {
	opts.NoReply = true
	future, err := c.CallAsync(ctx, method, req, header, opts)
	if err != nil {
		return err
	}
	_, err = future.Await(ctx)
	return err
}
