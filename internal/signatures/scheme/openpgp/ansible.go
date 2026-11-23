package openpgp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/guided-traffic/quader/internal/formats/untrusted"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki"
)

// The chain of an Ansible collection (R-SIG-031): the signature signs MANIFEST.json, MANIFEST.json names FILES.json by its SHA-256, and
// FILES.json names every file of the archive by its SHA-256. ansible-galaxy collection verify checks the same chain.
const (
	filesEntry = "FILES.json"
	// maxManifest and maxFiles bound the two documents read into memory: a MANIFEST.json is kilobytes, a FILES.json of ten thousand files
	// about two megabytes.
	maxManifest = 1 << 20
	maxFiles    = 16 << 20
)

// collectionManifest is what MANIFEST.json says of FILES.json.
type collectionManifest struct {
	FileManifest *collectionFile `json:"file_manifest_file"`
}

// collectionFiles is FILES.json.
type collectionFiles struct {
	Files []collectionFile `json:"files"`
}

// collectionFile is one entry of FILES.json, or MANIFEST.json's entry of FILES.json.
type collectionFile struct {
	Name         string `json:"name"`
	Type         string `json:"ftype"`
	ChecksumType string `json:"chksum_type"`
	SHA256       string `json:"chksum_sha256"`
}

// collection is what one pass over a collection archive keeps: the two documents, the SHA-256 of every other file and the targets of its
// links, which are names and are never read through (formats/ansible.md Q-ANS-91).
type collection struct {
	manifest, files []byte
	sums            map[string]string
	links           map[string]string
}

// ansibleManifest reads the collection archive once, checks FILES.json against MANIFEST.json and every file against FILES.json, and
// answers MANIFEST.json, the content the signature signs. A mismatch anywhere is digest_mismatch (R-SIG-031).
func ansibleManifest(ctx context.Context, in scheme.Input) ([]byte, error) {
	c, err := readCollection(ctx, in)
	if err != nil {
		return nil, err
	}
	if c.manifest == nil {
		return nil, pki.Invalid(signatures.ReasonDigestMismatch, "the collection archive holds no MANIFEST.json")
	}
	var m collectionManifest
	if err := decodeJSON(c.manifest, in.Limits, &m); err != nil {
		return nil, err
	}
	if m.FileManifest == nil || m.FileManifest.Name != filesEntry || c.files == nil {
		return nil, pki.Invalid(signatures.ReasonDigestMismatch, "MANIFEST.json names no FILES.json the archive holds")
	}
	if !sumMatches(c.files, *m.FileManifest) {
		return nil, pki.Invalid(signatures.ReasonDigestMismatch, "FILES.json is not the one MANIFEST.json names")
	}
	var f collectionFiles
	if err := decodeJSON(c.files, in.Limits, &f); err != nil {
		return nil, err
	}
	if err := filesMatch(f, c.sums, c.links); err != nil {
		return nil, err
	}
	return c.manifest, nil
}

// filesMatch checks every file FILES.json names against the archive, and that the archive holds no file FILES.json does not name. A
// file that is a link is named with the digest of its target, as ansible-galaxy collection build lists it, and the target's digest is
// the one compared; the targets stay in the archive's count of files, since FILES.json names them too.
func filesMatch(f collectionFiles, sums, links map[string]string) error {
	matched := map[string]bool{}
	for _, e := range f.Files {
		if e.Type != "file" {
			continue
		}
		name := trimEntry(e.Name)
		at := name
		if _, isLink := links[name]; isLink {
			target, ok := untrusted.ResolveLink(links, name)
			if !ok {
				return pki.Invalid(signatures.ReasonDigestMismatch, "the link %q names no file of the archive", name)
			}
			at = target
			delete(links, name)
		} else {
			matched[name] = true
		}
		got, ok := sums[at]
		if !ok || !strings.EqualFold(e.ChecksumType, "sha256") || !strings.EqualFold(got, e.SHA256) {
			return pki.Invalid(signatures.ReasonDigestMismatch, "the file %q is not the one FILES.json names", name)
		}
	}
	for name := range sums {
		if !matched[name] {
			return pki.Invalid(signatures.ReasonDigestMismatch, "the archive holds %q, which FILES.json does not name", name)
		}
	}
	for name := range links {
		return pki.Invalid(signatures.ReasonDigestMismatch, "the archive holds the link %q, which FILES.json does not name", name)
	}
	return nil
}

func sumMatches(data []byte, e collectionFile) bool {
	s := sha256.Sum256(data)
	return strings.EqualFold(e.ChecksumType, "sha256") && strings.EqualFold(hex.EncodeToString(s[:]), e.SHA256)
}

func decodeJSON(data []byte, l scheme.Limits, v any) error {
	if err := untrusted.CheckJSON(data, untrusted.Limits{JSONDepth: l.JSONDepth, JSONMembers: l.JSONValues}); err != nil {
		return pki.TooLarge("a document of the collection is out of the JSON bounds")
	}
	if err := json.Unmarshal(data, v); err != nil {
		return pki.Malformed("a document of the collection does not parse")
	}
	return nil
}

// readCollection streams the archive once within signatures.max_content_size and the bounds of untrusted.NewTarGz.
func readCollection(ctx context.Context, in scheme.Input) (*collection, error) {
	blob := in.ContentBlob
	if blob == ([32]byte{}) {
		blob = in.Subject
	}
	content, err := pki.Open(ctx, in, blob)
	if err != nil {
		return nil, err
	}
	if content.Size() > in.Limits.MaxContentSize {
		return nil, pki.Unsupported(signatures.ReasonLimitsExceeded, "the collection archive is larger than signatures.max_content_size")
	}
	r, err := content.Reader()
	if err != nil {
		return nil, pki.Transient(err)
	}
	defer r.Close()
	tr := untrusted.NewTarGz(ctx, r, untrusted.Limits{StreamBytes: in.Limits.MaxContentSize, UncompressedBytes: maxManifest + maxFiles},
		untrusted.ContainedSymlinks())
	c := &collection{sums: map[string]string{}, links: map[string]string{}}
	for {
		e, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return c, nil
		}
		if err != nil {
			return nil, archiveError(ctx, err)
		}
		if e.Dir {
			continue
		}
		if e.Link {
			c.links[trimEntry(e.Name)] = e.Target
			continue
		}
		if err := c.add(tr, trimEntry(e.Name)); err != nil {
			return nil, archiveError(ctx, err)
		}
	}
}

// add keeps the two documents and the SHA-256 of every other file.
func (c *collection) add(tr *untrusted.TarReader, name string) error {
	var err error
	switch name {
	case manifestEntry:
		c.manifest, err = tr.ReadEntry(maxManifest)
	case filesEntry:
		c.files, err = tr.ReadEntry(maxFiles)
	default:
		h := sha256.New()
		if _, err = tr.Stream(h); err == nil {
			c.sums[name] = hex.EncodeToString(h.Sum(nil))
		}
	}
	return err
}

// archiveError is the refusal of an archive the bounded reader refused, or the check's own end.
func archiveError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return pki.Transient(ctx.Err())
	}
	if untrusted.IsTooLarge(err) {
		return pki.TooLarge("the collection archive is beyond the bounds of its reader")
	}
	return pki.Malformed("the collection archive cannot be read")
}
