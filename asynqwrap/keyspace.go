package asynqwrap

import (
	"strings"

	"github.com/otpki/otpki-pkg/config"
)

func prefixedStatePath(cfg config.Config, parts ...string) string {
	all := append([]string{"asynqwrap"}, parts...)
	return cfg.App.Keyspace.Key(all...)
}

func prefixedTopicPath(cfg config.Config, parts ...string) string {
	return strings.ReplaceAll(prefixedStatePath(cfg, parts...), "/", ".")
}
