package offline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/s3"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/outbound"
)

// Info is what the tool knows of the connection it reads: its id (the connection of its name key), its prefix, and whether it is
// encrypted with keyed names, from the connection element or the directory's pull.
type Info struct {
	Connection string `json:"connection"`
	Prefix     string `json:"prefix"`
	Encrypted  bool   `json:"encrypted"`
	KeyedNames bool   `json:"keyedNames"`
}

// Object is an object of a source: its name (its key in the bucket, or its path below the directory) and its size.
type Object struct {
	Name string
	Size int64
}

// Source is where the objects are read from: a bucket through a connection element, or a directory written by Pull. It writes
// nothing.
type Source interface {
	Info() Info
	// Walk calls fn for each object whose name starts with under, in the order of their names.
	Walk(ctx context.Context, under string, fn func(Object) error) error
	// Head returns an object's size and metadata (nil for none); a missing object is blob.ErrNotFound.
	Head(ctx context.Context, name string) (Object, map[string]string, error)
	// Open returns an object's bytes.
	Open(ctx context.Context, name string) (io.ReadCloser, error)
}

// Bucket is the source of a bucket reached through objects (the S3 client of a connection element, or an in-memory store).
func Bucket(objects blob.ObjectStore, info Info) Source { return bucket{objects: objects, info: info} }

type bucket struct {
	objects blob.ObjectStore
	info    Info
}

func (b bucket) Info() Info { return b.info }

func (b bucket) Walk(ctx context.Context, under string, fn func(Object) error) error {
	token := ""
	for {
		page, err := b.objects.List(ctx, under, blob.ListOptions{Token: token})
		if err != nil {
			return err
		}
		for _, o := range page.Objects {
			if err := fn(Object{Name: o.Key, Size: o.Size}); err != nil {
				return err
			}
		}
		if page.Next == "" {
			return nil
		}
		token = page.Next
	}
}

func (b bucket) Head(ctx context.Context, name string) (Object, map[string]string, error) {
	info, err := b.objects.Head(ctx, name)
	if err != nil {
		return Object{}, nil, err
	}
	return Object{Name: name, Size: info.Size}, info.Metadata, nil
}

func (b bucket) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	body, _, err := b.objects.Get(ctx, name, nil)
	return body, err
}

// maxElement bounds a connection element's file.
const maxElement = 64 << 10

// OpenElement reads the connection element of storage.md R-STOR-050 in the file path (one element: id, bucket, prefix, endpoint,
// region, credentials, ca_file, encrypted, keyed_names, …), checks it as the binary checks storage.connections, reads its *_file
// keys, and returns the client of its bucket with the element's facts. Nothing of the installation's configuration, its database or
// its environment is read; the shared client settings (storage.object_store) and the outbound settings (proxy.outbound: the system's
// trust store, no proxy) take their defaults, and the element's ca_file is trusted beside them.
func OpenElement(ctx context.Context, path string) (blob.ObjectStore, Info, error) {
	el, cfg, err := loadElement(path)
	if err != nil {
		return nil, Info{}, err
	}
	c, err := s3.NewFromFactory(ctx, config.ClientConfig(0, el, cfg.Storage.ObjectStore), outbound.New(cfg.Proxy.Outbound))
	if err != nil {
		return nil, Info{}, refuse("the connection element %s: %v", path, err)
	}
	return c, Info{Connection: el.ID, Prefix: el.KeyPrefix(), Encrypted: el.Encrypted, KeyedNames: el.UsesKeyedNames()}, nil
}

// loadElement reads the element through the configuration's own loader, as the only element of storage.connections of a
// configuration that holds nothing else, so its rules and defaults are the binary's.
func loadElement(path string) (config.StorageConnection, *config.Config, error) {
	raw, err := readBounded(path, maxElement)
	if err != nil {
		return config.StorageConnection{}, nil, refuse("the connection element %s cannot be read: %v", path, err)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil || len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return config.StorageConnection{}, nil, refuse("the file %s is not a connection element: a YAML map of the keys of storage.md R-STOR-050 (example: {id: archive, bucket: quader-archive, encrypted: true})", path)
	}
	doc := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "storage"},
		{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "connections"},
			{Kind: yaml.SequenceNode, Content: []*yaml.Node{node.Content[0]}},
		}},
	}}
	file, err := yaml.Marshal(doc)
	if err != nil {
		return config.StorageConnection{}, nil, err
	}
	loaded, err := config.Load(config.Options{
		File:    path,
		Environ: []string{},
		Command: config.CommandImport,
		ReadFile: func(p string) ([]byte, error) {
			if p == path {
				return file, nil
			}
			return os.ReadFile(p) //nolint:gosec // G304: a file the element names (bucket_file, a credential's file, ca_file)
		},
	})
	if err != nil {
		return config.StorageConnection{}, nil, refuse("the connection element %s: %v", path, err)
	}
	conns, err := config.ResolveConnections(loaded.Config.Storage.Connections, nil)
	if err != nil {
		return config.StorageConnection{}, nil, refuse("the connection element %s: %v", path, err)
	}
	return conns[0], loaded.Config, nil
}

func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // G304: the operator names the file
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("it is larger than %d bytes", limit)
	}
	return b, nil
}

// The directory written by Pull: each object at <dir>/<object name>, its metadata beside it as <object name>.record.json (H-1303:
// a copy that drops the metadata loses every data key), and the pull's facts in <dir>/quader-pull.json.
const (
	// ManifestFile is the file of the pull's facts at the top of the directory.
	ManifestFile = "quader-pull.json"
	// RecordSuffix ends the file of an object's metadata.
	RecordSuffix = ".record.json"
	// manifestFormat is the format of ManifestFile.
	manifestFormat = "quader-offline-pull-v1"
	// maxRecordFile bounds a record file: an object's metadata is at most 2 KiB (blob.MaxMetadataBytes).
	maxRecordFile = 16 << 10
)

// manifest is the content of ManifestFile.
type manifest struct {
	Format   string    `json:"format"`
	PulledAt time.Time `json:"pulledAt"`
	Info
}

// Dir is the source of a directory written by Pull: it reads the pull's facts from ManifestFile.
func Dir(dir string) (Source, error) {
	raw, err := readBounded(filepath.Join(dir, ManifestFile), maxRecordFile)
	if err != nil {
		return nil, refuse("the directory %s was not written by quader admin storage offline pull: its %s cannot be read: %v", dir, ManifestFile, err)
	}
	var m manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil || m.Format != manifestFormat || m.Connection == "" {
		return nil, refuse("the file %s in %s is not the record of a pull (format %s)", ManifestFile, dir, manifestFormat)
	}
	return directory{root: dir, info: m.Info}, nil
}

type directory struct {
	root string
	info Info
}

func (d directory) Info() Info { return d.info }

// path is the file of an object name; a name that is not a local path is refused, so no name leads out of the directory.
func (d directory) path(name string) (string, error) {
	p := filepath.FromSlash(name)
	if !filepath.IsLocal(p) || strings.HasSuffix(name, RecordSuffix) || name == ManifestFile {
		return "", refuse("%q is no object name of the directory %s", name, d.root)
	}
	return filepath.Join(d.root, p), nil
}

func (d directory) Walk(ctx context.Context, under string, fn func(Object) error) error {
	start := filepath.Join(d.root, filepath.FromSlash(path.Dir(under+"x")))
	err := filepath.WalkDir(start, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && p == start {
				return fs.SkipAll
			}
			return err
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if !e.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(d.root, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if !strings.HasPrefix(name, under) || strings.HasSuffix(name, RecordSuffix) || name == ManifestFile {
			return nil
		}
		fi, err := e.Info()
		if err != nil {
			return err
		}
		return fn(Object{Name: name, Size: fi.Size()})
	})
	return err
}

func (d directory) Head(_ context.Context, name string) (Object, map[string]string, error) {
	p, err := d.path(name)
	if err != nil {
		return Object{}, nil, err
	}
	fi, err := os.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Object{}, nil, blob.ErrNotFound
	case err != nil:
		return Object{}, nil, err
	case !fi.Mode().IsRegular():
		return Object{}, nil, refuse("%s is not a regular file", p)
	}
	md, err := readRecordFile(p + RecordSuffix)
	if err != nil {
		return Object{}, nil, err
	}
	return Object{Name: name, Size: fi.Size()}, md, nil
}

// readRecordFile reads an object's metadata written by Pull: a JSON object of strings. A missing file is no metadata, as an object
// a copy that dropped it would be.
func readRecordFile(p string) (map[string]string, error) {
	raw, err := readBounded(p, maxRecordFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, refuse("the record file %s cannot be read: %v", p, err)
	}
	var md map[string]string
	if err := json.Unmarshal(raw, &md); err != nil {
		return nil, refuse("the record file %s is not a JSON object of the object's metadata: %s", p, jsonProblem(err))
	}
	return md, nil
}

func (d directory) Open(_ context.Context, name string) (io.ReadCloser, error) {
	p, err := d.path(name)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p) //nolint:gosec // G304: a path below the directory the operator named, checked by d.path
	if errors.Is(err, fs.ErrNotExist) {
		return nil, blob.ErrNotFound
	}
	return f, err
}
