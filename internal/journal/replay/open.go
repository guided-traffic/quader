package replay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"

	"go.yaml.in/yaml/v3"

	"github.com/guided-traffic/quader/internal/blob/s3"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/outbound"
)

// CredentialsFile is the credentials file of `--credentials-file` and `--journal-credentials-file` (R-HA-025 step 5,
// R-BAK-015 "Journal reader", R-BAK-030): one credential per connection, each an element with the keys of a connection's
// credential (R-STOR-031) and the connection it reads.
//
//	connections:
//	  - connection: default
//	    credentials_source: static
//	    access_key_id_file: /var/run/secrets/quader/s3-default-journal/access-key-id
//	    secret_access_key_file: /var/run/secrets/quader/s3-default-journal/secret-access-key
type CredentialsFile struct {
	Connections []config.ConnectionCredentials `yaml:"connections"`
}

// For returns the credential the file names for a connection, false when it names none.
func (f CredentialsFile) For(connection string) (config.StorageS3Credentials, bool) {
	for _, c := range f.Connections {
		if c.Connection == connection {
			return c.Credentials(), true
		}
	}
	return config.StorageS3Credentials{}, false
}

// LoadCredentials reads a credentials file. Unknown keys are refused, so a misspelled key cannot silently fall back to the
// runtime credential, and so is an element that breaks the rules of R-STOR-031 or names a connection twice.
func LoadCredentials(path string) (CredentialsFile, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the operator names the credentials file on the command line
	if err != nil {
		return CredentialsFile{}, fmt.Errorf("reading the credentials file: %w", err)
	}
	var f CredentialsFile
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return CredentialsFile{}, fmt.Errorf("the credentials file %s: %w", path, err)
	}
	var errs []error
	config.CheckCredentialElements(f.Connections, func(i int, id, msg string) {
		errs = append(errs, fmt.Errorf("the credentials file %s: connections[%d] (%s): %s", path, i, id, msg))
	})
	if len(f.Connections) == 0 {
		errs = append(errs, fmt.Errorf("the credentials file %s names no connection: it holds connections, a list of {connection, credentials_source, …}", path))
	}
	if err := errors.Join(errs...); err != nil {
		return CredentialsFile{}, err
	}
	return f, nil
}

// Location is the journal of one connection to read: the connection's id and the client configuration that reaches the bucket
// and the prefix of its journal with the blob-store credential.
type Location struct {
	Connection string
	Client     config.StorageS3
}

// OpenJournal builds the reader of the journal on every location, merged by key (storage.md R-STOR-059). A connection that the
// credentials file names is read with that credential and lists object versions as well, so a delete marker set with the runtime
// credential cannot hide an entry (R-HA-054); any other, and every connection without a file, is read with the blob-store
// credential of its client configuration and the plain listing, because the runtime credential deliberately lacks
// s3:ListBucketVersions (R-HA-023).
func OpenJournal(ctx context.Context, locs []Location, out *outbound.Factory, credentialsFile string) (*Reader, error) {
	if len(locs) == 0 {
		return nil, errors.New("journal store: no connection to read the journal from")
	}
	var file CredentialsFile
	if credentialsFile != "" {
		f, err := LoadCredentials(credentialsFile)
		if err != nil {
			return nil, err
		}
		file = f
	}
	sources := make([]Source, 0, len(locs))
	for _, l := range locs {
		cfg := l.Client
		creds, versions := file.For(l.Connection)
		if versions {
			cfg.Credentials = creds
		}
		client, err := s3.NewFromFactory(ctx, cfg, out)
		if err != nil {
			return nil, fmt.Errorf("journal store of connection %s: %w", l.Connection, err)
		}
		sources = append(sources, Source{Connection: l.Connection, Store: s3.NewJournalStore(client, versions), Prefix: cfg.Prefix})
	}
	return ReaderOf(sources...), nil
}
