package config

import "time"

// Limits configures the counting of the download limits (usage-and-limits.md R-LIM-017 to R-LIM-028). The profiles, rules,
// ceilings, the mode and the block time are not configuration: they are stored and changed through the API (R-LIM-031 to
// R-LIM-033).
type Limits struct {
	FlushInterval time.Duration  `yaml:"flush_interval" default:"1s" validate:"min=100ms" doc:"How often a replica carries the bytes and refused requests it counted to the counters in Valkey (R-LIM-017)."`
	Parallel      LimitsParallel `yaml:"parallel"`
	Pacing        LimitsPacing   `yaml:"pacing"`
	Usage         LimitsUsage    `yaml:"usage"`
}

// LimitsParallel configures the parallel downloads (R-LIM-024).
type LimitsParallel struct {
	Lease time.Duration `yaml:"lease" default:"60s" validate:"min=10s" doc:"How long a download's lease holds its place without a renewal; a crashed replica's leases end after it."`
	Wait  time.Duration `yaml:"wait" default:"10s" doc:"How long a further download waits for a free place before it answers 429; 0 answers at once."`
}

// LimitsPacing configures the slowing of a download (R-LIM-023).
type LimitsPacing struct {
	MinBytesPerSecond int64 `yaml:"min_bytes_per_second" default:"65536" validate:"min=1024" doc:"Every download proceeds at least at this rate, whatever its share, so no client's idle timeout fires."`
}

// LimitsUsage configures the usage store (R-LIM-027, R-LIM-028).
type LimitsUsage struct {
	FlushInterval    time.Duration `yaml:"flush_interval" default:"10s" validate:"min=1s" doc:"How often a replica writes its usage buffer to PostgreSQL."`
	MaxBufferedKeys  int           `yaml:"max_buffered_keys" default:"100000" validate:"min=1000" doc:"Rows a replica's usage buffer holds; beyond, a new key is merged into an overflow row."`
	SummaryRetention time.Duration `yaml:"summary_retention" default:"9600h" validate:"min=24h" doc:"How long the daily sums without a person are kept (400 days)."`
}
