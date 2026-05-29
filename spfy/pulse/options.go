package pulse

import (
	"time"

	"github.com/samber/oops"
)

type Reliability uint8

const (
	Ephemeral Reliability = iota
	Durable
)

func (r Reliability) String() string {
	switch r {
	case Durable:
		return "durable"
	default:
		return "ephemeral"
	}
}

func (r *Reliability) UnmarshalText(text []byte) error {
	switch string(text) {
	case "", "ephemeral":
		*r = Ephemeral
	case "durable":
		*r = Durable
	default:
		return oops.Errorf("invalid reliability %q", text)
	}
	return nil
}

type Delivery uint8

const (
	Fanout Delivery = iota
	WorkQueue
)

func (d Delivery) String() string {
	switch d {
	case WorkQueue:
		return "work_queue"
	default:
		return "fanout"
	}
}

func (d *Delivery) UnmarshalText(text []byte) error {
	switch string(text) {
	case "", "fanout":
		*d = Fanout
	case "work_queue":
		*d = WorkQueue
	default:
		return oops.Errorf("invalid delivery %q", text)
	}
	return nil
}

type PublishOptions struct {
	Reliability Reliability
}

type SubscribeOptions struct {
	Delivery    Delivery
	Group       string
	Reliability Reliability
}

type RequestOptions struct {
	Timeout     time.Duration
	Reliability Reliability
	NoReply     bool
}

type Capabilities struct {
	DurablePubSub bool
	DurableRPC    bool
	Fanout        bool
	WorkQueue     bool
	NativeRPC     bool
}

func NormalizeSubscribeOptions(subject string, opts SubscribeOptions) SubscribeOptions {
	if opts.Delivery == WorkQueue && opts.Group == "" {
		opts.Group = DefaultGroup(subject)
	}
	return opts
}

func NormalizeRequestOptions(opts RequestOptions) RequestOptions {
	return opts
}
