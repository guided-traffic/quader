package iface

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// The bounds of the reporting batch's usage and status (R-BAS-016, R-LIM-045, Q-LIM-308).
const (
	// MaxUsageRows bounds the rows of one batch; the Bastion reports what lies beyond them per repository and subject kind
	// in [UsageReport.Overflow].
	MaxUsageRows = 100_000
	// MaxStatusBlocks bounds the active blocks of a status, the newest first.
	MaxStatusBlocks = 100
)

// UsageReport is the usage member of a reporting batch (R-LIM-045): what the Bastion counted per subject, credential and
// repository in buckets of BucketSeconds. Rows and Overflow are written as [] when empty.
type UsageReport struct {
	BucketSeconds int             `json:"bucketSeconds"`
	Rows          []UsageRow      `json:"rows"`
	Overflow      []UsageOverflow `json:"overflow"`
}

// MarshalJSON writes the report with [] for empty lists: the members are always present.
func (u UsageReport) MarshalJSON() ([]byte, error) {
	type plain UsageReport
	p := plain(u)
	if p.Rows == nil {
		p.Rows = []UsageRow{}
	}
	if p.Overflow == nil {
		p.Overflow = []UsageOverflow{}
	}
	return json.Marshal(p)
}

// UsageRow is one subject's use of one repository through the Bastion in one bucket (R-LIM-045).
type UsageRow struct {
	BucketStart time.Time `json:"bucketStart"`
	// Subject is the subject key ([SubjectLimits.Key], or the Bastion's anonymous:<address prefix>).
	Subject string `json:"subject"`
	// Credential is the presented token's ID, empty for an anonymous caller.
	Credential string `json:"credential"`
	// Repository is the qualified name "<projectKey>/<repoKey>".
	Repository       string `json:"repository"`
	Rule             string `json:"rule"`
	Requests         int64  `json:"requests"`
	Refused          int64  `json:"refused"`
	CachedBytes      int64  `json:"cachedBytes"`
	UncachedBytes    int64  `json:"uncachedBytes"`
	UpstreamRequests int64  `json:"upstreamRequests"`
	// Limited counts refusals per figure ([FigureNames]); WouldLimit what Audit would have refused.
	Limited    map[string]int64 `json:"limited"`
	WouldLimit map[string]int64 `json:"wouldLimit"`
}

// UsageOverflow is what a batch counted beyond [MaxUsageRows] rows, per repository and subject kind (Q-LIM-308).
type UsageOverflow struct {
	BucketStart      time.Time `json:"bucketStart"`
	SubjectKind      string    `json:"subjectKind"`
	Repository       string    `json:"repository"`
	Subjects         int64     `json:"subjects"`
	Requests         int64     `json:"requests"`
	Refused          int64     `json:"refused"`
	CachedBytes      int64     `json:"cachedBytes"`
	UncachedBytes    int64     `json:"uncachedBytes"`
	UpstreamRequests int64     `json:"upstreamRequests"`
}

// Validate checks the bounds: a bucket length not below 0, at most [MaxUsageRows] rows, counts not below 0, and only
// figure names as the keys of limited and wouldLimit.
func (u UsageReport) Validate() error {
	if u.BucketSeconds < 0 {
		return fmt.Errorf("bastion usage: bucketSeconds %d is negative", u.BucketSeconds)
	}
	if len(u.Rows) > MaxUsageRows {
		return fmt.Errorf("bastion usage: %d rows, at most %d", len(u.Rows), MaxUsageRows)
	}
	names := FigureNames()
	for i, r := range u.Rows {
		if r.Requests < 0 || r.Refused < 0 || r.CachedBytes < 0 || r.UncachedBytes < 0 || r.UpstreamRequests < 0 {
			return fmt.Errorf("bastion usage: row %d has a negative count", i)
		}
		for _, m := range []map[string]int64{r.Limited, r.WouldLimit} {
			for k, v := range m {
				if !slices.Contains(names, k) || v < 0 {
					return fmt.Errorf("bastion usage: row %d counts %q %d", i, k, v)
				}
			}
		}
	}
	for i, o := range u.Overflow {
		if o.Subjects < 0 || o.Requests < 0 || o.Refused < 0 || o.CachedBytes < 0 || o.UncachedBytes < 0 || o.UpstreamRequests < 0 {
			return fmt.Errorf("bastion usage: overflow %d has a negative count", i)
		}
	}
	return nil
}

// ReplicaStatus is the sending replica's state (R-UI-240): its cache, its throughput, who is limited and blocked now.
// Blocks is written as [] when empty.
type ReplicaStatus struct {
	// Replica is the pod name ([HeaderReplica]) and At the time of the status.
	Replica    string           `json:"replica"`
	At         time.Time        `json:"at"`
	Cache      CacheStatus      `json:"cache"`
	Throughput ThroughputStatus `json:"throughput"`
	// Limited and Blocked count the subjects refused by a budget and the addresses blocked within the batch's window.
	Limited int64         `json:"limited"`
	Blocked int64         `json:"blocked"`
	Blocks  []StatusBlock `json:"blocks"`
}

// MarshalJSON writes the status with [] for no block: the member is always present.
func (s ReplicaStatus) MarshalJSON() ([]byte, error) {
	type plain ReplicaStatus
	p := plain(s)
	if p.Blocks == nil {
		p.Blocks = []StatusBlock{}
	}
	return json.Marshal(p)
}

// CacheStatus is a replica's cache (R-BAS-031, R-BAS-098).
type CacheStatus struct {
	Bytes          int64 `json:"bytes"`
	CapacityBytes  int64 `json:"capacityBytes"`
	Hits           int64 `json:"hits"`
	Misses         int64 `json:"misses"`
	Evictions      int64 `json:"evictions"`
	ProtectedBytes int64 `json:"protectedBytes"`
}

// ThroughputStatus is a replica's throughput (R-BAS-097): whether the cap holds a read back now, and the rate per class.
type ThroughputStatus struct {
	Limited               bool              `json:"limited"`
	BytesPerSecondByClass ThroughputClasses `json:"bytesPerSecondByClass"`
}

// ThroughputClasses are a figure per class of the throughput (R-BAS-097).
type ThroughputClasses struct {
	Anonymous int64 `json:"anonymous"`
	SignedIn  int64 `json:"signedIn"`
	Catalogue int64 `json:"catalogue"`
}

// StatusBlock is one active block of an address (R-BAS-037): the prefix, its end and why.
type StatusBlock struct {
	Address string    `json:"address"`
	Until   time.Time `json:"until"`
	Reason  string    `json:"reason"`
}

// Validate checks the bounds: at most [MaxStatusBlocks] blocks, each with a canonical prefix, and counts not below 0.
func (s ReplicaStatus) Validate() error {
	if len(s.Blocks) > MaxStatusBlocks {
		return fmt.Errorf("bastion status: %d blocks, at most %d", len(s.Blocks), MaxStatusBlocks)
	}
	for _, b := range s.Blocks {
		if !canonicalPrefix(b.Address) {
			return fmt.Errorf("bastion status: the block's address %q is no canonical prefix", b.Address)
		}
	}
	c := s.Cache
	if s.Limited < 0 || s.Blocked < 0 || c.Bytes < 0 || c.CapacityBytes < 0 || c.Hits < 0 || c.Misses < 0 || c.Evictions < 0 || c.ProtectedBytes < 0 {
		return fmt.Errorf("bastion status: a negative count")
	}
	return nil
}
