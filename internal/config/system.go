package config

import "time"

// System configures system information and the replica registry (R-OPS-002 to
// R-OPS-004).
type System struct {
	PublicVersion          bool          `yaml:"public_version" default:"false" doc:"Expose the version to unauthenticated callers."`
	BootstrapAnonymousRead bool          `yaml:"bootstrap_anonymous_read" default:"false" doc:"Development only: true lets anonymous callers read /api/v1/system/info and /api/v1/system/replicas on the default endpoint while it admits anonymous requests. Refused while the base URL of an endpoint is an https origin other than localhost."`
	HeartbeatInterval      time.Duration `yaml:"heartbeat_interval" default:"10s" validate:"min=1s"`
	HeartbeatStaleAfter    time.Duration `yaml:"heartbeat_stale_after" default:"30s" validate:"min=1s"`
}

// Validate checks the cross-field rules of the section.
func (s *System) Validate(r *Report) {
	if s.HeartbeatStaleAfter < 2*s.HeartbeatInterval {
		r.Add("heartbeat_stale_after", "must be at least 2 times heartbeat_interval")
	}
}
