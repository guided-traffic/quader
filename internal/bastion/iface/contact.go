package iface

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// EdgeLimit is the edge figure of the ingress in front of a Bastion's replica (R-BAS-093), the value of
// [HeaderEdgeLimit]: the requests a second and the connections per client address the ingress admits. A member of 0 is a
// figure the edge does not set; both 0 is an edge without a limit, written "off".
type EdgeLimit struct {
	RequestsPerSecond int
	Connections       int
}

// Off reports whether the edge limits nothing.
func (e EdgeLimit) Off() bool { return e.RequestsPerSecond <= 0 && e.Connections <= 0 }

// Header formats the value of [HeaderEdgeLimit]: "rps=<n>; connections=<n>" with the members that are set, or "off".
func (e EdgeLimit) Header() string {
	if e.Off() {
		return edgeOff
	}
	var parts []string
	if e.RequestsPerSecond > 0 {
		parts = append(parts, "rps="+strconv.Itoa(e.RequestsPerSecond))
	}
	if e.Connections > 0 {
		parts = append(parts, "connections="+strconv.Itoa(e.Connections))
	}
	return strings.Join(parts, "; ")
}

const edgeOff = "off"

// errEdgeLimit is the error of a [HeaderEdgeLimit] value that is not of the form of [EdgeLimit.Header].
var errEdgeLimit = errors.New("bastion contact: malformed " + HeaderEdgeLimit)

// ParseEdgeLimit reads a value of [HeaderEdgeLimit]: "off", or one or both of rps=<n> and connections=<n>, each at most
// once, each a positive decimal without a sign or a leading zero.
func ParseEdgeLimit(v string) (EdgeLimit, error) {
	v = strings.TrimSpace(v)
	if v == edgeOff {
		return EdgeLimit{}, nil
	}
	var e EdgeLimit
	for _, part := range strings.Split(v, ";") {
		k, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		n, good := positiveDecimal(val)
		if !ok || !good {
			return EdgeLimit{}, fmt.Errorf("%w: %q", errEdgeLimit, v)
		}
		switch {
		case k == "rps" && e.RequestsPerSecond == 0:
			e.RequestsPerSecond = n
		case k == "connections" && e.Connections == 0:
			e.Connections = n
		default:
			return EdgeLimit{}, fmt.Errorf("%w: %q", errEdgeLimit, v)
		}
	}
	return e, nil
}

// positiveDecimal reads a positive int written without a sign or a leading zero.
func positiveDecimal(s string) (int, bool) {
	if s == "" || s[0] == '0' || len(s) > 9 {
		return 0, false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// ParseCatalogueKept reads a value of [HeaderCatalogueKept]: exactly "true" or "false".
func ParseCatalogueKept(v string) (bool, error) {
	switch v {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("bastion contact: malformed %s %q", HeaderCatalogueKept, v)
}
