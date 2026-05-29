package rpc

import (
	"strings"

	"github.com/otpki/otpki-pkg/spfy/pulse"
)

type Method struct {
	Service string
	Name    string
}

func (m Method) Subject() string {
	service := strings.Trim(m.Service, "/")
	name := strings.Trim(m.Name, "/")
	if service == "" {
		return name
	}
	if name == "" {
		return service
	}
	return service + "/" + name
}

func (m Method) Group() string {
	if m.Service == "" {
		return pulse.DefaultGroup(m.Subject())
	}
	return pulse.SafeName(m.Service)
}
