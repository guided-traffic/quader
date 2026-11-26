package app

import (
	"context"

	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/downloadlinks"
)

// AreaDownloadLinks is the delivery by signed links of the object store (storage.md R-STOR-014, R-STOR-070 to R-STOR-074): the
// store of the installation's switch, the replica's snapshot of it, each connection's state with its link check, and the signing
// of a link. Every role with a client listener builds it: the format host asks it at the serve point, and the management API answers
// the settings with this replica's view of its connections. The worker serves no download and builds none. It holds the handover
// per blob to a Bastion as well (R-STOR-095), with the replica's snapshot of the registrations whose handover is on (R-BAS-071).
const AreaDownloadLinks Area = "download_links"

// downloadLinksArea is the result of the step: the store the operations of the management API write through, the replica's
// side the serve point asks, and the snapshot of the Bastions' switches the serve point and the handshake read.
type downloadLinksArea struct {
	store *downloadlinks.Store
	links *downloadlinks.Links
	// bastions is the replica's snapshot of the Bastion registrations: the switches of the handover and the networks each Bastion
	// connects from, which the recognition of a Bastion's request reads (R-BAS-017).
	bastions *bastionmain.RegistrationSnapshot
}

// wireDownloadLinks builds the store over the main pool, the replica's side over the blob store's connections, the snapshot
// component, which reads the row at start and on every change of the area download_link_settings and runs each connection's link
// check at start and when the switch is turned on, and the snapshot of the Bastion registrations, which follows the areas
// bastion_handover and bastion_egress and runs the link checks again when a registration's handover is turned on (R-STOR-096).
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
	bastions := bastionmain.NewRegistrationSnapshot(in.db, in.caches, a.log.With("component", "bastion"))
	// The handover of an encrypted copy takes its data key from the blob store (the one caller of its method besides the store,
	// R-STOR-096) and writes the key's trace through the main pool and the audit's recorder before the answer (R-STOR-095 step 10).
	links := downloadlinks.New(downloadlinks.Options{
		Connections: bl.store.Connections(), Copies: bl.store.Copies, Rule: downloadlinks.NewHostRule(a.cfg.Server),
		Handovers: bastions, DataKeys: bl.store, Runner: in.db, Audit: in.rec, Replica: a.id.Instance, Metrics: m, Log: log,
	})
	snap := downloadlinks.NewSnapshot(links, store, in.caches)
	if err := a.startComponent(ctx, snap); err != nil {
		return downloadLinksArea{}, err
	}
	bastions.OnSwitchedOn(snap.CheckLinks)
	if err := a.startComponent(ctx, bastions); err != nil {
		return downloadLinksArea{}, err
	}
	return downloadLinksArea{store: store, links: links, bastions: bastions}, nil
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
