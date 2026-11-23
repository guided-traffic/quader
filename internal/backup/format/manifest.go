package format

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ManifestFormat is the format number this binary writes and the highest it reads.
const ManifestFormat = 1

// Manifest is manifest.json (R-BAK-003), the last object of a backup. A reader ignores unknown fields and refuses a
// higher format.
type Manifest struct {
	Format           int         `json:"format"`
	BackupID         ID          `json:"backupId"`
	InstallationID   string      `json:"installationId"`
	Incarnation      string      `json:"incarnation"`
	Quader           QuaderInfo  `json:"quader"`
	Schema           SchemaInfo  `json:"schema"`
	Postgres         PostgresRef `json:"postgres"`
	StartedAt        time.Time   `json:"startedAt"`
	SnapshotAt       time.Time   `json:"snapshotAt"`
	CompletedAt      time.Time   `json:"completedAt"`
	PreviousBackupID *ID         `json:"previousBackupId"`
	Options          Options     `json:"options"`
	Source           Source      `json:"source"`
	Tables           []TableFile `json:"tables"`
	Sequences        []Sequence  `json:"sequences"`
	Blobs            BlobsInfo   `json:"blobs"`
	Secrets          SecretsInfo `json:"secrets"`
	// StorageKeys names the storage keys the backup needs and holds (R-BAK-003, R-BAK-032); nil in a manifest written before them.
	StorageKeys *StorageKeysInfo `json:"storageKeys,omitempty"`
	Encryption  Encryption       `json:"encryption"`
}

// StorageKeysInfo is the manifest's storageKeys (R-BAK-032): Pool, counted from the final blob list, the storage keys its encrypted
// pool objects are wrapped under; Sealed and Destroyed, read from storage_keys inside the snapshot, the identifiers its database holds
// sealed and destroyed (storage.md R-STOR-097). The retention listing copies Pool and Sealed of every retained backup, which the
// destruction of a retired connection's keys reads (storage.md R-STOR-100).
type StorageKeysInfo struct {
	Pool      []PoolKey `json:"pool"`
	Sealed    []string  `json:"sealed"`
	Destroyed []string  `json:"destroyed"`
}

// PoolKey is one storage key of the pool: the encrypted pool objects the blob list names under it and their bytes as stored.
type PoolKey struct {
	KeyID   string `json:"keyId"`
	Objects int64  `json:"objects"`
	Bytes   int64  `json:"bytes"`
}

// PoolKeyIDs is the identifiers of Pool, nil for a manifest without storageKeys.
func (m *Manifest) PoolKeyIDs() []string {
	if m.StorageKeys == nil {
		return nil
	}
	out := make([]string, 0, len(m.StorageKeys.Pool))
	for _, k := range m.StorageKeys.Pool {
		out = append(out, k.KeyID)
	}
	return out
}

// SealedKeyIDs is Sealed, nil for a manifest without storageKeys.
func (m *Manifest) SealedKeyIDs() []string {
	if m.StorageKeys == nil {
		return nil
	}
	return append([]string{}, m.StorageKeys.Sealed...)
}

// QuaderInfo names the build that took the backup.
type QuaderInfo struct {
	Version  string `json:"version"`
	Revision string `json:"revision"`
}

// SchemaInfo is the migration state inside the snapshot: the highest applied step and every applied step with the SHA-256
// of its file from schema_migration (R-BAK-003, R-BAK-023).
type SchemaInfo struct {
	Step  int          `json:"step"`
	Steps []SchemaStep `json:"steps"`
}

// SchemaStep is one applied migration step.
type SchemaStep struct {
	Step   int    `json:"step"`
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// PostgresRef records the server the snapshot was taken on.
type PostgresRef struct {
	ServerVersion string `json:"serverVersion"`
	Isolation     string `json:"isolation"`
}

// Options are the run options that change what the backup holds.
type Options struct {
	ExcludeRemoteCaches bool `json:"excludeRemoteCaches"`
}

// Source says where the source's connections were (no credentials) and whether its revocation journal was enabled, so that
// a restore finds the journal it must replay on each of them (R-BAK-024, R-BAK-030).
type Source struct {
	Connections       []ConnectionRef `json:"connections"`
	RevocationJournal bool            `json:"revocationJournal"`
}

// ConnectionRef is the location of one object-store connection of the source: its id, its endpoint (empty for the AWS
// endpoint of its region), its bucket and its prefix (R-BAK-030).
type ConnectionRef struct {
	ID       string `json:"id"`
	Endpoint string `json:"endpoint"`
	Bucket   string `json:"bucket"`
	Prefix   string `json:"prefix"`
}

// TableFile is one exported table (R-BAK-002, R-BAK-003). Columns are [name, type] pairs in the order of the file.
// SHA256, Bytes and CompressedBytes are computed while the file streams; Rows counts what COPY produced.
type TableFile struct {
	Name            string      `json:"name"`
	Class           string      `json:"class"`
	File            string      `json:"file"`
	Columns         [][2]string `json:"columns"`
	Rows            int64       `json:"rows"`
	Bytes           int64       `json:"bytes"`
	CompressedBytes int64       `json:"compressedBytes"`
	SHA256          string      `json:"sha256"`
}

// Sequence is the state of one sequence at the end of the export.
type Sequence struct {
	Name      string `json:"name"`
	LastValue int64  `json:"lastValue"`
	IsCalled  bool   `json:"isCalled"`
}

// BlobsInfo describes the blob list and the copy.
type BlobsInfo struct {
	File            string        `json:"file"`
	SHA256          string        `json:"sha256"`
	Referenced      int64         `json:"referenced"`
	ReferencedBytes int64         `json:"referencedBytes"`
	Copied          int64         `json:"copied"`
	CopiedBytes     int64         `json:"copiedBytes"`
	Damaged         []DamagedBlob `json:"damaged"`
}

// DamagedBlob is a digest whose source object was missing or had the wrong size during the run. It is in the blob list and
// not in the pool (R-BAK-006). Status is the blobs.content_status value the restore loads.
type DamagedBlob struct {
	SHA256 string `json:"sha256"`
	Status string `json:"status"`
}

// SecretsInfo lists the key IDs the sealed values of the snapshot need. Key material is never written.
type SecretsInfo struct {
	KeyIDs []string `json:"keyIds"`
}

// Encryption records the protection of the files on the target (R-BAK-026, R-BAK-027). SSE is the mode actually applied:
// "AES256", "aws:kms" or "none". ClientSide is nil or "age"; Recipients are the age public keys.
type Encryption struct {
	SSE        string   `json:"sse"`
	ClientSide *string  `json:"clientSide"`
	Recipients []string `json:"recipients,omitempty"`
}

// Encode is the JSON form of the manifest, indented, newline-terminated.
func (m *Manifest) Encode() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// DecodeManifest parses a manifest. A higher format than ManifestFormat yields an [*UnknownFormatError] (exit 6); unknown
// fields are ignored, as R-BAK-003 requires.
func DecodeManifest(b []byte) (*Manifest, error) {
	var head struct {
		Format int `json:"format"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if head.Format > ManifestFormat {
		return nil, &UnknownFormatError{What: "manifest", Got: head.Format, Known: ManifestFormat}
	}
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate checks the shape a reader relies on, not the content of the files.
func (m *Manifest) Validate() error {
	var errs []error
	if m.Format < 1 {
		errs = append(errs, errors.New("format must be at least 1"))
	}
	if _, err := ParseID(string(m.BackupID)); err != nil {
		errs = append(errs, err)
	}
	if m.InstallationID == "" || m.Incarnation == "" {
		errs = append(errs, errors.New("installationId and incarnation are required"))
	}
	if m.Schema.Step < 1 || len(m.Schema.Steps) == 0 {
		errs = append(errs, errors.New("schema.step and schema.steps are required"))
	}
	if m.SnapshotAt.IsZero() || m.StartedAt.IsZero() || m.CompletedAt.IsZero() {
		errs = append(errs, errors.New("startedAt, snapshotAt and completedAt are required"))
	}
	seen := map[string]bool{}
	for _, t := range m.Tables {
		switch {
		case t.Name == "" || t.File == "":
			errs = append(errs, errors.New("every table needs a name and a file"))
		case seen[t.Name]:
			errs = append(errs, fmt.Errorf("table %s is listed twice", t.Name))
		case t.Class != "data" && t.Class != "blobs":
			errs = append(errs, fmt.Errorf("table %s has class %q; only data and blobs tables are exported", t.Name, t.Class))
		case len(t.Columns) == 0:
			errs = append(errs, fmt.Errorf("table %s has no columns", t.Name))
		case len(t.SHA256) != 64:
			errs = append(errs, fmt.Errorf("table %s has no sha256", t.Name))
		}
		seen[t.Name] = true
	}
	if m.Blobs.File == "" || len(m.Blobs.SHA256) != 64 {
		errs = append(errs, errors.New("blobs.file and blobs.sha256 are required"))
	}
	for _, d := range m.Blobs.Damaged {
		if _, err := ParseDigest(d.SHA256); err != nil {
			errs = append(errs, fmt.Errorf("blobs.damaged: %w", err))
		}
		if d.Status != "missing" && d.Status != "corrupt" {
			errs = append(errs, fmt.Errorf("blobs.damaged: status %q is not missing or corrupt", d.Status))
		}
	}
	ids := map[string]bool{}
	for _, c := range m.Source.Connections {
		switch {
		case c.ID == "" || c.Bucket == "":
			errs = append(errs, errors.New("every source connection needs an id and a bucket"))
		case ids[c.ID]:
			errs = append(errs, fmt.Errorf("source connection %s is listed twice", c.ID))
		}
		ids[c.ID] = true
	}
	if len(m.Secrets.KeyIDs) == 0 {
		// A snapshot always holds at least the canary rows of the key ring (R-ARCH-021).
		errs = append(errs, errors.New("secrets.keyIds is empty"))
	}
	return errors.Join(append(errs, m.StorageKeys.validate()...)...)
}

// validate checks the identifiers and counts of the manifest's storageKeys (R-BAK-032); a manifest without the block has none to check.
func (sk *StorageKeysInfo) validate() []error {
	if sk == nil {
		return nil
	}
	var errs []error
	for _, k := range sk.Pool {
		if !ValidPoolKey(k.KeyID) || k.Objects < 0 || k.Bytes < 0 {
			errs = append(errs, fmt.Errorf("storageKeys.pool: %q with %d objects of %d bytes", k.KeyID, k.Objects, k.Bytes))
		}
	}
	for _, id := range append(append([]string{}, sk.Sealed...), sk.Destroyed...) {
		if !ValidPoolKey(id) {
			errs = append(errs, fmt.Errorf("storageKeys: %q is no key identifier", id))
		}
	}
	return errs
}

// DamagedSet returns the damaged digests as a set of hex strings.
func (m *Manifest) DamagedSet() map[string]string {
	out := make(map[string]string, len(m.Blobs.Damaged))
	for _, d := range m.Blobs.Damaged {
		out[d.SHA256] = d.Status
	}
	return out
}

// Table returns the entry of a table.
func (m *Manifest) Table(name string) (TableFile, bool) {
	for _, t := range m.Tables {
		if t.Name == name {
			return t, true
		}
	}
	return TableFile{}, false
}
