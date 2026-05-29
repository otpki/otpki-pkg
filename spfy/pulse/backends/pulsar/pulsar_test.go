package pulsar

import (
	"testing"

	"github.com/otpki/otpki-pkg/spfy/pulse"
)

func TestSubscriptionNameWorkQueueUsesStableGroup(t *testing.T) {
	b := &Backend{cfg: normalizeConfig(Config{})}
	opts := pulse.NormalizeSubscribeOptions("subject", pulse.SubscribeOptions{Delivery: pulse.WorkQueue, Group: "workers"})

	first := b.subscriptionName("subject", opts, false)
	second := b.subscriptionName("subject", opts, false)

	if first != second {
		t.Fatalf("work queue subscribers must share a subscription name: %q != %q", first, second)
	}
}

func TestSubscriptionNameFanoutUsesUniqueSubscribers(t *testing.T) {
	b := &Backend{cfg: normalizeConfig(Config{})}
	opts := pulse.SubscribeOptions{Delivery: pulse.Fanout}

	first := b.subscriptionName("subject", opts, false)
	second := b.subscriptionName("subject", opts, false)

	if first == second {
		t.Fatalf("fanout subscribers must use independent subscription names: %q", first)
	}
}
