package observability

import "net/url"

// Scrubber stands in for the scrubber of the real package.
type Scrubber struct{}

// URL renders an address.
func (Scrubber) URL(u *url.URL) string { return u.Host }

// ScrubURL renders an address.
func ScrubURL(u *url.URL) string { return u.Host }

// Other renders nothing of the rule.
func Other(u *url.URL) string { return u.String() }
