package gui

import (
	"context"

	"github.com/guided-traffic/quader/internal/bastion/catalogue"
)

// docRoutes are the routes of a package's documentation pages that the sitemap lists beside the package's page in a project (W-35's
// B.11: the newest version's documentation entries while docs is on), each a route below the locale as page.Address.Route writes it,
// with its query. This is the hook of the documentation through the web interface (W-35's P11), which fills it; until then a package
// lists its page alone.
func (h *Handler) docRoutes(_ context.Context, _ catalogue.Entry, _ string) []string { return nil }
