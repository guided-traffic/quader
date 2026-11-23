package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/offline"
)

const storageOfflineUsage = `usage: quader admin storage offline list   --keys <export.json> --key-ring <dir> (--connection-file <element.yaml> | --dir <path>) [--json]
       quader admin storage offline get    --keys <export.json> --key-ring <dir> (--connection-file <element.yaml> | --dir <path>) <sha256 or object name> --out <file> [--json]
       quader admin storage offline verify --keys <export.json> --key-ring <dir> (--connection-file <element.yaml> | --dir <path>) [--concurrency N] [--json]
       quader admin storage offline pull   --connection-file <element.yaml> --to <dir> [--json]

Reads the objects of an encrypted connection without a database, Valkey or a running Quader: the last resort when the database is
lost and no backup holds the blobs (storage.md R-STOR-089). It writes nothing to a bucket.

list    open the record of every object below keyed/, sha256/ and pool/sha256/ (one HeadObject each) and print its name, stored size,
        plaintext size and digest, the storage key and ok, plain, name_mismatch, unknown_key, destroyed_key or the record's refusal
get     decrypt one blob, by its SHA-256 or its object name, into --out, a file that must not exist; the file appears only after every
        segment, the final segment and the SHA-256 passed, and a failure writes nothing
verify  read every object whole as the scrub does and print one line per object
pull    copy the objects with each one's metadata as <object name>.record.json into --to, an empty or new directory, for list, get
        and verify with --dir (a copy that drops the metadata loses every data key); needs no key

--keys       the export of the storage keys (quader admin storage keys export)
--key-ring   the directory of the key ring files that sealed the export (<key id>.key, the key ring Secret's escrow copy)
--connection-file  one element of storage.connections in a file: id, bucket, prefix, endpoint, region, credentials, ca_file, encrypted

exit codes: 0 every object is sound (or written), 1 failed, 2 usage, or an export, key ring, element, --out or --to that does not fit,
5 an object is refused (damage, a missing or destroyed key, a foreign name), or get's blob is not there, 75 temporary (the store did not answer)`

// offlineDeps are what the offline commands reach outside the process: the client of a connection element's bucket, and the clock
// of a pull. The unit tests give an in-memory store.
type offlineDeps struct {
	openElement func(ctx context.Context, file string) (blob.ObjectStore, offline.Info, error)
	now         func() time.Time
}

// storageOfflineCmd is quader admin storage offline <command> (R-STOR-089).
func storageOfflineCmd(args []string, s streams) error {
	return runStorageOffline(args, s, offlineDeps{openElement: offline.OpenElement, now: time.Now})
}

func runStorageOffline(args []string, s streams, d offlineDeps) error {
	if len(args) == 0 {
		return usageError{storageOfflineUsage}
	}
	o, err := parseOfflineFlags(args[0], args[1:], s)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return offlineExit(o.run(ctx, d, s))
}

// offlineFlags are the flags of one offline command.
type offlineFlags struct {
	cmd                              string
	keys, keyRing, connFile, dir, to string
	out, ref                         string
	concurrency                      int
	json                             bool
}

// parseOfflineFlags parses the flags of cmd; a positional argument (get's SHA-256 or object name) may stand before or after them.
// The key ring and the export are named by files only: no flag takes a key, so none lands in a shell's history or a process list.
func parseOfflineFlags(cmd string, args []string, s streams) (*offlineFlags, error) {
	switch cmd {
	case "list", "get", "verify", "pull":
	default:
		return nil, usageError{storageOfflineUsage}
	}
	o := &offlineFlags{cmd: cmd}
	fs := flag.NewFlagSet("quader admin storage offline "+cmd, flag.ContinueOnError)
	fs.SetOutput(s.stderr)
	fs.StringVar(&o.connFile, "connection-file", "", "a connection element of storage.connections in a file")
	fs.BoolVar(&o.json, "json", false, "print the result as JSON on standard output")
	if cmd == "pull" {
		fs.StringVar(&o.to, "to", "", "the empty or new directory the objects and their records are copied into")
	} else {
		fs.StringVar(&o.keys, "keys", "", "the export of the storage keys")
		fs.StringVar(&o.keyRing, "key-ring", "", "the directory of the key ring files that sealed the export")
		fs.StringVar(&o.dir, "dir", "", "a directory written by quader admin storage offline pull")
	}
	if cmd == "get" {
		fs.StringVar(&o.out, "out", "", "the file the plaintext is written to; it must not exist")
	}
	if cmd == "verify" {
		fs.IntVar(&o.concurrency, "concurrency", offline.DefaultConcurrency, "objects read at once")
	}
	var pos []string
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		pos, args = append(pos, args[0]), args[1:]
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, err
		}
		return nil, usageError{err.Error() + "\n" + storageOfflineUsage}
	}
	pos = append(pos, fs.Args()...)
	if !o.complete(pos) {
		return nil, usageError{storageOfflineUsage}
	}
	return o, nil
}

// complete takes the positional arguments and reports whether the flags the command needs are given.
func (o *offlineFlags) complete(pos []string) bool {
	if o.cmd == "get" {
		if len(pos) != 1 || o.out == "" {
			return false
		}
		o.ref = pos[0]
	} else if len(pos) != 0 {
		return false
	}
	if o.cmd == "pull" {
		return o.connFile != "" && o.to != ""
	}
	return o.keys != "" && o.keyRing != "" && (o.connFile == "") != (o.dir == "") && (o.cmd != "verify" || o.concurrency >= 1)
}

// source opens the bucket of the connection element or the directory.
func (o *offlineFlags) source(ctx context.Context, d offlineDeps) (offline.Source, error) {
	if o.dir != "" {
		return offline.Dir(o.dir)
	}
	objects, info, err := d.openElement(ctx, o.connFile)
	if err != nil {
		return nil, err
	}
	return offline.Bucket(objects, info), nil
}

func (o *offlineFlags) run(ctx context.Context, d offlineDeps, s streams) error {
	if o.cmd == "pull" {
		return o.pull(ctx, d, s.stdout)
	}
	ks, err := offline.LoadKeys(o.keys, o.keyRing)
	if err != nil {
		return err
	}
	defer ks.Close()
	src, err := o.source(ctx, d)
	if err != nil {
		return err
	}
	r := offline.NewReader(src, ks)
	if o.cmd == "get" {
		return o.get(ctx, r, s.stdout)
	}
	p := newResultPrinter(s.stdout, o.json, o.cmd, src.Info())
	var t offline.Totals
	if o.cmd == "list" {
		t, err = r.List(ctx, p.result)
	} else {
		t, err = r.Verify(ctx, o.concurrency, p.result)
	}
	if perr := p.end(t, err); perr != nil && err == nil {
		err = perr
	}
	if err != nil {
		return err
	}
	return walkExit(o.cmd, t)
}

// walkExit is the exit of list and verify: 5 with a finding, 75 when an object could not be read and none was refused.
func walkExit(cmd string, t offline.Totals) error {
	switch {
	case t.Findings > 0:
		return backup.Fail(backup.ExitFindings, backup.CodeFindings,
			fmt.Errorf("%s: %d of %d objects are refused or have a foreign name", cmd, t.Findings, t.Objects))
	case t.Unread > 0:
		return backup.Fail(backup.ExitTemporary, backup.CodeTemporary, fmt.Errorf("%s: %d of %d objects could not be read: the store did not answer", cmd, t.Unread, t.Objects))
	}
	return nil
}

func (o *offlineFlags) get(ctx context.Context, r *offline.Reader, w io.Writer) error {
	name, err := r.Locate(ctx, o.ref)
	if errors.Is(err, blob.ErrNotFound) {
		return backup.Fail(backup.ExitFindings, backup.CodeFindings, fmt.Errorf("get: no object of %s below keyed/, sha256/ or pool/sha256/; nothing was written", o.ref))
	}
	if err != nil {
		return err
	}
	res, err := r.Get(ctx, name, o.out)
	if err != nil {
		return err
	}
	if o.json {
		if err := writeOfflineJSON(w, res); err != nil {
			return err
		}
	}
	if !res.Sound() {
		if !o.json {
			if err := writeResult(w, res); err != nil {
				return err
			}
		}
		if res.Status == offline.StatusUnread {
			return backup.Fail(backup.ExitTemporary, backup.CodeTemporary, fmt.Errorf("get: %s could not be read; nothing was written", name))
		}
		return backup.Fail(backup.ExitFindings, backup.CodeFindings, fmt.Errorf("get: %s is refused (%s); nothing was written", name, res.Status))
	}
	if o.json {
		return nil
	}
	key := ""
	if res.KeyID != "" {
		key = ", key " + res.KeyID
	}
	_, err = fmt.Fprintf(w, "wrote %s: %d bytes, sha256 %s, from %s%s\n", o.out, *res.Size, res.SHA256, name, key)
	return err
}

func (o *offlineFlags) pull(ctx context.Context, d offlineDeps, w io.Writer) error {
	src, err := o.source(ctx, d)
	if err != nil {
		return err
	}
	t, err := offline.Pull(ctx, src, o.to, d.now(), func(name string, size int64) error {
		if o.json {
			return nil
		}
		_, err := fmt.Fprintf(w, "pulled %s: %d bytes\n", name, size)
		return err
	})
	if err != nil {
		return err
	}
	if o.json {
		return writeOfflineJSON(w, t)
	}
	_, err = fmt.Fprintf(w, "storage offline pull: %d objects, %d bytes into %s; %d objects with a name outside the layout skipped\n", t.Objects, t.Bytes, o.to, t.Skipped)
	return err
}

// offlineExit makes the errors of the offline commands exit as the usage says: a refusal of an input 2, a store that does not
// answer 75, a store that refuses the credential or knows no bucket 2.
func offlineExit(err error) error {
	switch {
	case err == nil:
		return nil
	case offline.IsRefusal(err), blob.IsConfig(err):
		return backup.Fail(backup.ExitUsage, backup.CodeConfig, err)
	case blob.IsTransient(err):
		return backup.Fail(backup.ExitTemporary, backup.CodeTemporary, err)
	}
	return backupExit(err)
}

// resultPrinter prints the results of list and verify as lines, or as one JSON document that it streams: the connection, the objects
// as they come and the totals last.
type resultPrinter struct {
	w     io.Writer
	json  bool
	cmd   string
	first bool
}

func newResultPrinter(w io.Writer, asJSON bool, cmd string, info offline.Info) *resultPrinter {
	p := &resultPrinter{w: w, json: asJSON, cmd: cmd, first: true}
	if asJSON {
		head, _ := json.Marshal(info)
		_, _ = fmt.Fprintf(w, "{\"source\": %s,\n \"objects\": [", head)
	}
	return p
}

func (p *resultPrinter) result(r offline.Result) error {
	if !p.json {
		return writeResult(p.w, r)
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	sep := ",\n  "
	if p.first {
		sep, p.first = "\n  ", false
	}
	_, err = fmt.Fprintf(p.w, "%s%s", sep, b)
	return err
}

// end prints the totals; after a failed walk the JSON document still closes, with the error.
func (p *resultPrinter) end(t offline.Totals, walkErr error) error {
	if !p.json {
		_, err := fmt.Fprintf(p.w, "storage offline %s: %d objects, %d ok, %d plain, %d refused or of a foreign name, %d unread\n",
			p.cmd, t.Objects, t.OK, t.Plain, t.Findings, t.Unread)
		return err
	}
	tail, err := json.Marshal(t)
	if err != nil {
		return err
	}
	msg := "null"
	if walkErr != nil {
		b, _ := json.Marshal(walkErr.Error())
		msg = string(b)
	}
	_, err = fmt.Fprintf(p.w, "],\n \"totals\": %s,\n \"error\": %s}\n", tail, msg)
	return err
}

// writeResult prints one object: its status and name, the stored size, the plaintext's size and digest and the storage key where
// known, and the detail of a refusal.
func writeResult(w io.Writer, r offline.Result) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s stored=%d", r.Status, r.Name, r.StoredSize)
	if r.Size != nil {
		fmt.Fprintf(&b, " size=%d", *r.Size)
	}
	if r.SHA256 != "" {
		fmt.Fprintf(&b, " sha256=%s", r.SHA256)
	}
	if r.KeyID != "" {
		fmt.Fprintf(&b, " key=%s", r.KeyID)
	}
	if r.Detail != "" {
		fmt.Fprintf(&b, ": %s", r.Detail)
	}
	b.WriteByte('\n')
	_, err := io.WriteString(w, b.String())
	return err
}

func writeOfflineJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
