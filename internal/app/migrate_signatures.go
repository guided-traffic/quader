package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
	"github.com/guided-traffic/quader/internal/signatures/job"
	"github.com/guided-traffic/quader/internal/signatures/shipped"
	sigstore "github.com/guided-traffic/quader/internal/signatures/store"
)

// maxAnchorFile bounds a file of anchor material the chart mounts: the largest material of R-SIG-015, an own Sigstore root, is 1 MiB.
const maxAnchorFile = 1 << 20

// applySignatureAnchors is the part of `quader migrate`'s declared state that belongs to the signatures (signatures.md R-SIG-017,
// R-SIG-018): the release's shipped anchors and the anchors of the configuration, in one transaction. An anchor of the configuration
// whose material fails the checks of R-SIG-015 stops the migration with its key and the reason; one no longer declared is removed. Both
// raise the anchor epoch when anything changed, which wakes the trust job (R-SIG-012).
func applySignatureAnchors(ctx context.Context, s *postgres.MigrationSession, changes *cluster.Changes, recorder audit.Recorder, cfg *config.Config, log *slog.Logger) error {
	declared, err := declaredAnchors(cfg.Signatures.Anchors)
	if err != nil {
		return err
	}
	st := sigstore.New(sigstore.Options{Runner: s, Audit: recorder, Changes: changes, WakeTrust: job.WakeTrust})
	if err := s.InTx(ctx, func(tx meta.Tx) error {
		if err := st.ApplyShipped(ctx, tx, shipped.Anchors()); err != nil {
			return err
		}
		return st.ApplyConfiguration(ctx, tx, sigstore.ConfigurationActor(), declared)
	}); err != nil {
		return fmt.Errorf("signatures.anchors: %w", err)
	}
	log.Info("trust anchors of the release and the configuration applied", "shipped", len(shipped.Anchors()), "declared", len(declared))
	return nil
}

// declaredAnchors turns the configuration's anchors into the store's declarations, in key order, reading the material the chart mounts
// as files (R-SIG-018).
func declaredAnchors(in map[string]config.SignatureAnchor) ([]sigstore.Declaration, error) {
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]sigstore.Declaration, 0, len(keys))
	for _, k := range keys {
		m, err := anchorMaterial(in[k])
		if err != nil {
			return nil, fmt.Errorf("signatures.anchors.%s: %w", k, err)
		}
		out = append(out, sigstore.Declaration{Key: k, Title: in[k].Title, Kind: in[k].Kind, Material: m})
	}
	return out, nil
}

// anchorMaterial is one declared anchor's material in the shape of anchors.Material.
func anchorMaterial(a config.SignatureAnchor) (anchors.Material, error) {
	var m anchors.Material
	var err error
	switch {
	case a.CA != nil:
		m.CA, err = caMaterial(*a.CA)
	case a.OpenPGP != nil:
		var keys string
		if keys, err = inlineOrFile(a.OpenPGP.Keys, a.OpenPGP.KeysFile); err == nil {
			m.OpenPGP = &anchors.OpenPGPMaterial{Keys: keys}
		}
	case a.PublicKey != nil:
		m.PublicKey, err = publicKeyMaterial(*a.PublicKey)
	case a.Sigstore != nil:
		m.Sigstore, err = sigstoreMaterial(*a.Sigstore)
	}
	return m, err
}

func caMaterial(c config.SignatureAnchorCA) (*anchors.CAMaterial, error) {
	certs, err := inlineOrFile(c.Certificates, c.CertificatesFile)
	if err != nil {
		return nil, err
	}
	return &anchors.CAMaterial{Certificates: certs, Use: c.Use, Subject: c.Subject, SubjectAltNames: c.SubjectAltNames}, nil
}

func publicKeyMaterial(p config.SignatureAnchorPublicKey) (*anchors.PublicKeyMaterial, error) {
	out := &anchors.PublicKeyMaterial{}
	for i, k := range p.Keys {
		pem, err := inlineOrFile(k.PEM, k.PEMFile)
		if err != nil {
			return nil, fmt.Errorf("keys[%d]: %w", i, err)
		}
		e := anchors.PublicKeyEntry{PEM: pem, KeyID: k.KeyID}
		if k.Expires != "" {
			t, err := time.Parse(time.RFC3339, k.Expires)
			if err != nil {
				return nil, fmt.Errorf("keys[%d].expires: %w", i, err)
			}
			e.Expires = &t
		}
		out.Keys = append(out.Keys, e)
	}
	return out, nil
}

func sigstoreMaterial(s config.SignatureAnchorSigstore) (*anchors.SigstoreMaterial, error) {
	out := &anchors.SigstoreMaterial{
		Issuer: s.Issuer, Identity: s.Identity, IdentityPattern: s.IdentityPattern, TrustRoot: s.EffectiveTrustRoot(),
	}
	if s.TrustedRoot != "" || s.TrustedRootFile != "" {
		doc, err := inlineOrFile(s.TrustedRoot, s.TrustedRootFile)
		if err != nil {
			return nil, err
		}
		out.TrustedRoot = []byte(doc)
	}
	return out, nil
}

// inlineOrFile is the material given inline, or the content of the file the chart mounted, at most maxAnchorFile bytes.
func inlineOrFile(inline, file string) (string, error) {
	if file == "" {
		return inline, nil
	}
	f, err := os.Open(filepath.Clean(file))
	if err != nil {
		return "", fmt.Errorf("the material file: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxAnchorFile+1))
	if err != nil {
		return "", fmt.Errorf("the material file %s: %w", file, err)
	}
	if len(data) > maxAnchorFile {
		return "", fmt.Errorf("the material file %s is larger than 1 MiB", file)
	}
	return string(data), nil
}
