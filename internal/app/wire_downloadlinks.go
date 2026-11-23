package app

import (
	"context"

	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/downloadlinks"
)

// AreaDownloadLinks is the delivery by signed links of the object store (storage.md R-STOR-014, R-STOR-070 to R-STOR-074): the
// store of the installation's switch, the replica's snapshot of it, each connection's state with its link check, and the signing
// of a link. Every role with a client listener builds it: the format host asks it at the serve point, and the management API answers
// the settings with this replica's view of its connections. The worker serves no download and builds none.
const AreaDownloadLinks Area = "download_links"

// downloadLinksArea is the result of the step: the store the operations of the management API write through, and the replica's
// side the serve point asks.
type downloadLinksArea struct {
	store *downloadlinks.Store
	links *downloadlinks.Links
}

// wireDownloadLinks builds the store over the main pool, the replica's side over the blob store's connections, and the snapshot
// component, which reads the row at start and on every change of the area download_link_settings and runs each connection's link
// check at start and when the switch is turned on.
func (a *App) wireDownloadLinks(ctx context.Context, in infra, bl blobArea) (downloadLinksArea, error) {
	if err := requireInputs(AreaDownloadLinks, given("database", in.db), given("blob store", bl.store)); err != nil {
		return downloadLinksArea{}, err
	}
	log := a.log.With("component", "download-links")
	store := downloadlinks.NewStore(downloadlinks.StoreOptions{Runner: in.db, Changes: in.changes, Audit: in.rec, Log: log})
	var m *downloadlinks.Metrics
	if a.tele != nil {
		m = downloadlinks.NewMetrics(a.tele.metrics)
	}
	links := downloadlinks.New(downloadlinks.Options{
		Connections: bl.store.Connections(), Copies: bl.store.Copies, Rule: downloadlinks.NewHostRule(a.cfg.Server),
		Replica: a.id.Instance, Metrics: m, Log: log,
	})
	if err := a.startComponent(ctx, downloadlinks.NewSnapshot(links, store, in.caches)); err != nil {
		return downloadLinksArea{}, err
	}
	return downloadLinksArea{store: store, links: links}, nil
}

// downloadLinksAPI is the services of the operations of the signed links (download_links_api.go): the store and the replica's
// side; a role without the area answers them with 503.
func (a *App) downloadLinksAPI() v1.DownloadLinksServices {
	dl := part[downloadLinksArea](a.parts)
	if dl.store == nil || dl.links == nil {
		return v1.DownloadLinksServices{}
	}
	return v1.DownloadLinksServices{Store: dl.store, Links: dl.links}
}
