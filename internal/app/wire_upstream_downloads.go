package app

import (
	"context"

	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/fills"
	"github.com/guided-traffic/quader/internal/proxy"
)

// AreaUpstreamDownloads is the overview of the downloads from the upstreams of remote repositories
// (operations-and-observability.md R-OPS-049 to R-OPS-052): the tracker every remote engine of the process reports to, the publisher of
// its running and ended downloads, and the store the API reads.
const AreaUpstreamDownloads Area = "upstream_downloads"

// upstreamDownloadsArea is the result: one tracker per process, shared by the client engine (packages, web, all) and the background
// engine (worker, api, web, all), and the store.
type upstreamDownloadsArea struct {
	tracker *fills.Tracker
	store   *fills.Store
}

// downloads is the tracker as the remote engine takes it.
func (u upstreamDownloadsArea) downloads() proxy.DownloadTracker {
	if u.tracker == nil { // a nil *fills.Tracker in the interface would not be nil
		return nil
	}
	return u.tracker
}

// reader is the store as the API takes it.
func (u upstreamDownloadsArea) reader() v1.UpstreamDownloadReader {
	if u.store == nil {
		return nil
	}
	return u.store
}

// wireUpstreamDownloads builds the tracker and the store and starts the publisher on every serving role. A role whose engines fill
// nothing (api: its engine lends the upstream catalogue its upstream) writes no live row and only sweeps the rows of replicas that
// stopped (R-OPS-051). The publisher is a component: its Flush in the close step writes the last ends and removes the live row.
func (a *App) wireUpstreamDownloads(ctx context.Context, in infra) (upstreamDownloadsArea, error) {
	if err := requireInputs(AreaUpstreamDownloads, given("database", in.db)); err != nil {
		return upstreamDownloadsArea{}, err
	}
	tracker := fills.NewTracker(fills.TrackerOptions{
		Replica: a.id.Instance, Metrics: a.tele.metrics.Registry(), RepoLabel: a.tele.metrics.Repos().Value,
	})
	a.tele.metrics.Repos().OnForget(tracker.Forget)
	store := fills.NewStore(in.db)
	pub := fills.NewPublisher(fills.PublisherOptions{
		Tracker: tracker, Sink: store, InstanceUID: a.id.UID, Replica: a.id.Instance,
		Interval: a.cfg.Proxy.UpstreamDownloads.PublishInterval, Log: a.log.With("component", "upstream-downloads"),
	})
	if err := a.startComponent(ctx, pub); err != nil {
		return upstreamDownloadsArea{}, err
	}
	return upstreamDownloadsArea{tracker: tracker, store: store}, nil
}
