package app

import (
	"context"
	"crypto/tls"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/api/scanlisten"
	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/buildinfo"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/observability/health"
	"github.com/guided-traffic/quader/internal/outbound"
	"github.com/guided-traffic/quader/internal/scanreport"
)

// The scanner replicas (operations-and-observability.md R-OPS-048, Q-OPS-223, ADR-224). A replica of the role scanner reports itself to
// the scan listener of packages it already reaches with the certificate of its kind, and signs off at its clean shutdown
// (startScannerReport); the scan listener admits the report by that certificate (scannerKindOf) and writes the row
// (registerScannerReportRoutes); the management API lists the rows with the kinds of this replica's configuration (scannerSource).

// scannerReporter is the component of a replica of the role scanner that sends its reports, named after its unit
// (<unit>-report, Q-OPS-222). It is started after the scanner's own component, so it signs off before the scanner's link stops.
type scannerReporter struct {
	unit string
	r    *scanreport.Reporter
}

func (c *scannerReporter) Name() string { return c.unit + "-report" }

func (c *scannerReporter) Start(ctx context.Context) error {
	c.r.Start(ctx)
	return nil
}

// Drain sends the report draining at once (R-ARCH-024 step 1).
func (c *scannerReporter) Drain(context.Context) { c.r.Drain() }

// Stop signs off.
func (c *scannerReporter) Stop(ctx context.Context) error {
	c.r.Stop(ctx)
	return nil
}

// startScannerReport starts the reports of a replica of the role scanner. The role all runs no scanner pod and reports nothing, and
// neither does the loader of the image scanner's database: the scanner of its pod reports the database (Q-ARCH-564, Q-OPS-225).
func (a *App) startScannerReport(ctx context.Context) error {
	if a.id.Role != config.RoleScanner {
		return nil
	}
	s, ok := lookup[scannerArea](a.parts)
	if !ok || !s.built() || s.loader != nil {
		return nil
	}
	url, unit := a.cfg.Vulnerabilities.Images.Registry.URL, s.unit()
	if s.malware != nil || s.updater != nil {
		url = a.cfg.Malware.ScanListener.URL
	}
	var link func() *tls.Config
	if s.link != nil {
		link = s.link.Config
	}
	// The row's key in the canonical form of a UUID, as the registry lists its instance UIDs.
	uid := a.id.UID
	if u, err := uuid.Parse(uid); err == nil {
		uid = u.String()
	}
	bi := buildinfo.Get()
	r := scanreport.NewReporter(scanreport.Options{
		URL: url, HTTP: outbound.LinkClient(outbound.LinkOptions{TLS: link}),
		Identity: scanreport.Identity{UID: uid, Name: a.id.Instance, Node: a.id.Node, Version: bi.Version, Revision: bi.Commit},
		Status:   a.scannerStatus(s), Interval: a.cfg.System.HeartbeatInterval,
		// A part of the unit, logged as the unit (Q-OPS-222).
		Log: a.log.With("component", unit),
	})
	return a.startComponent(ctx, &scannerReporter{unit: unit, r: r})
}

// clamdVersionTimeout bounds the question for clamd's version: clamd answers no command while it loads a database (Q-MAL-89), and the
// report goes without the version then.
const clamdVersionTimeout = time.Second

// scannerStatus is what a scanner reports of itself: the state of its readiness and the reason of the check that fails, the version of
// its tool, the signature database clamd scans with or the newest database the image scanner's pod holds with its build time
// (Q-OPS-225), and its slots; the updater the set its directory holds with the daily's build time and no slots (Q-OPS-224). clamd's
// version is asked until it answers once and kept: a pod's clamd does not change its version.
func (a *App) scannerStatus(s scannerArea) func(context.Context) scanreport.Status {
	var clamd atomic.Pointer[string]
	return func(ctx context.Context) scanreport.Status {
		snap := a.health.Snapshot()
		st := scanreport.Status{State: string(snap.State), Reason: failingReason(snap)}
		if s.updater != nil {
			st.ToolVersion, st.Database, st.DatabaseBuiltAt = s.updater.Status()
			return st
		}
		if s.malware != nil {
			st.InUse, st.Total, st.Database = s.malware.Busy(), s.malware.Slots(), s.malware.DatabaseLoaded()
			if v := clamd.Load(); v != nil {
				st.ToolVersion = *v
			} else {
				cctx, cancel := context.WithTimeout(ctx, clamdVersionTimeout)
				line, err := s.malware.ClamdVersion(cctx)
				cancel()
				if v := scanreport.ToolVersionOfClamd(line); err == nil && v != "" {
					clamd.Store(&v)
					st.ToolVersion = v
				}
			}
			return st
		}
		st.InUse, st.Total = s.svc.Busy(), s.svc.Slots()
		if s.analyser == nil {
			st.ToolVersion = s.svc.AnalyserVersion()
		}
		if hex, built := s.svc.Database(); hex != "" {
			st.Database = hex
			st.DatabaseBuiltAt = built
		}
		return st
	}
}

// failingReason is the reason of the first readiness check that fails, the synthetic draining left out (the state says it), or the
// check's name where it gave none; "" while none fails.
func failingReason(snap health.Snapshot) string {
	if snap.State == health.StateReady || snap.State == health.StateDraining {
		return ""
	}
	for _, c := range snap.Checks {
		if c.Status != health.Fail || c.Name == health.CheckDraining {
			continue
		}
		if c.Reason != "" {
			return c.Reason
		}
		return c.Name
	}
	return ""
}

// scannerKindOf is the kind of scanner a request's client certificate names (Q-OPS-223, Q-OPS-224): trivy for the name
// vulnerabilities.images.registry.client_name, clamav for malware.scan_listener.client_name, freshclam for
// malware.scan_listener.updater_client_name, "" for none, for a certificate that lists two, and for a request without TLS. The route of
// the scanner replicas admits a kind and takes it from here, never from the body.
func (a *App) scannerKindOf() func(*http.Request) string {
	names := map[string]string{}
	if n := a.cfg.Vulnerabilities.Images.Registry.ClientName; n != "" {
		names[n] = config.ScannerKindTrivy
	}
	if n := a.cfg.Malware.ScanListener.ClientName; n != "" {
		names[n] = config.ScannerKindClamav
	}
	if n := a.cfg.Malware.ScanListener.UpdaterClientName; n != "" {
		names[n] = config.ScannerKindFreshclam
	}
	return func(r *http.Request) string {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			return ""
		}
		kind := ""
		for name, k := range names {
			if httpserver.HasName(r.TLS.PeerCertificates[0], name) != nil {
				continue
			}
			if kind != "" && kind != k {
				return "" // a certificate of two kinds names none
			}
			kind = k
		}
		return kind
	}
}

// registerScannerReportRoutes mounts the route of the scanner replicas on the scan listener's router, on a replica with the database.
func (a *App) registerScannerReportRoutes(router *httpserver.Router) {
	db := a.database()
	if db == nil {
		return
	}
	scanlisten.RegisterReplicas(router, scanlisten.ReplicaOptions{
		Store: scannerStore{rows: cluster.ScannerReplicas{DB: db.Main(), System: a.cfg.System, MaxRows: scanreport.MaxRowsPerKind}},
		Kind:  a.scannerKindOf(), Log: a.log.With("component", "scan-listener"),
	})
}

// scannerStore is the route's store over the rows of the cluster package.
type scannerStore struct{ rows cluster.ScannerReplicas }

func (s scannerStore) Record(ctx context.Context, kind, uid string, r scanreport.Report) (bool, error) {
	rec := cluster.ScannerRecord{
		Name: r.Name, Node: r.Node, Version: r.Version, Revision: r.Revision, State: r.State, Reason: r.Reason,
		ToolVersion: r.ToolVersion, Database: r.Database, SlotsInUse: r.Slots.InUse, Slots: r.Slots.Total,
	}
	if r.DatabaseBuiltAt != nil {
		rec.DatabaseBuiltAt = *r.DatabaseBuiltAt
	}
	return s.rows.Record(ctx, kind, uid, rec)
}

func (s scannerStore) Remove(ctx context.Context, kind, uid string) (bool, error) {
	return s.rows.Remove(ctx, kind, uid)
}

// scannerSource lists the kinds of scanner and the scanner replicas for GET /api/v1/system/scanners.
type scannerSource struct{ a *App }

func (s scannerSource) Scanners(ctx context.Context) (gen.ScannerList, error) {
	out := gen.ScannerList{Kinds: scannerKinds(s.a.cfg), Items: []gen.ScannerReplica{}}
	db := s.a.database()
	if db == nil {
		return out, nil
	}
	var rows []cluster.ScannerReplica
	if err := db.Read(ctx, func(c q.DBTX) (err error) {
		rows, err = cluster.ListScannerReplicas(ctx, c, s.a.cfg.System)
		return err
	}); err != nil {
		return gen.ScannerList{}, err
	}
	for _, r := range rows {
		out.Items = append(out.Items, scannerReplicaOf(r))
	}
	return out, nil
}

// scannerKinds are the kinds of scanner whose scanning is on, the image scanning's first, each with whether its replicas report
// themselves (Q-OPS-223): a kind is called at an address (the chart sets both), whose replicas the scan listener can tell only by a
// certificate, which needs TLS and the kind's name. Without an address no scanner of the kind is configured: the role all builds none
// (Q-ARCH-563).
func scannerKinds(cfg *config.Config) []gen.ScannerKind {
	tlsOn := cfg.Server.TLS.Enabled()
	mode := func(url, name string) gen.ScannerKindReplicas {
		switch {
		case url == "":
			return gen.ScannerKindReplicasNone
		case !tlsOn || name == "":
			return gen.ScannerKindReplicasUnverified
		}
		return gen.ScannerKindReplicasReporting
	}
	out := []gen.ScannerKind{}
	if v := cfg.Vulnerabilities; v.Enabled && v.Images.Enabled {
		out = append(out, gen.ScannerKind{
			Kind: gen.ScannerKindKindTrivy, Deployment: "vuln-scanner-trivy", Purpose: gen.Vuln, Tool: "Trivy",
			Replicas: mode(v.Images.Scanner.URL, v.Images.Registry.ClientName),
		})
	}
	if m := cfg.Malware; m.Enabled {
		out = append(out, gen.ScannerKind{
			Kind: gen.ScannerKindKindClamav, Deployment: "malware-scanner-clamav", Purpose: gen.Malware, Tool: "ClamAV",
			Replicas: mode(m.Scanner.URL, m.ScanListener.ClientName),
		})
		if m.Database.Source == config.MalwareSourceUpdater {
			out = append(out, gen.ScannerKind{
				Kind: gen.ScannerKindKindFreshclam, Deployment: unitMalwareUpdaterFreshclam, Purpose: gen.Malware, Tool: "freshclam",
				Replicas: mode(m.Updater.URL, m.ScanListener.UpdaterClientName),
			})
		}
	}
	return out
}

func scannerReplicaOf(r cluster.ScannerReplica) gen.ScannerReplica {
	out := gen.ScannerReplica{
		Kind: gen.ScannerReplicaKind(r.Kind), InstanceUid: r.InstanceUID, Name: r.Name, Version: r.Version, Revision: r.Revision,
		State: gen.ReplicaState(r.State), StateSince: r.StateSince, Lost: r.Lost, StartedAt: r.StartedAt, HeartbeatAt: r.HeartbeatAt,
	}
	out.Slots.InUse, out.Slots.Total = r.SlotsInUse, r.Slots
	if r.Node != "" {
		out.Node = &r.Node
	}
	if r.Reason != "" {
		out.Reason = &r.Reason
	}
	if r.ToolVersion != "" {
		out.ToolVersion = &r.ToolVersion
	}
	if r.DatabaseVersion != "" {
		db := &gen.ScannerDatabase{Version: r.DatabaseVersion}
		if !r.DatabaseBuiltAt.IsZero() {
			built := r.DatabaseBuiltAt
			db.BuiltAt = &built
		}
		out.Database = db
	}
	return out
}
