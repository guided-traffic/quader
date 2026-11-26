package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// connectionKey is the key prefix of a connection's issues: "connections[<n>] (<id>):", which the report reads as
// "storage.connections[<n>] (<id>): …" (R-STOR-050).
func connectionKey(i int, id string) string { return fmt.Sprintf("connections[%d] (%s):", i, id) }

// validateConnections applies the start validation of R-STOR-050 that needs no file and no database: the number of
// elements, the ids, the exclusive keys, the prefix and the endpoint, the enumerations, the credential and encryption rules,
// the scrub's bounds, encrypted, drain and min_copies, and the location rule for the elements whose location is written in
// the configuration (an element with bucket_file, endpoint_file or region_file is checked when its files are read,
// [ResolveConnections]).
func (s *Storage) validateConnections(r *Report) {
	conns := s.Connections
	if len(conns) > MaxConnections {
		r.Add("connections", "holds %d elements; at most %d are allowed (Q-STOR-247)", len(conns), MaxConnections)
	}
	seen := map[string]int{}
	leftOut := -1
	nonDraining := 0
	for i, raw := range conns {
		c := raw.WithDefaults()
		key := connectionKey(i, c.ID)
		secondLeftOut := raw.ID == "" && leftOut >= 0
		switch {
		case secondLeftOut:
			r.Add(key, "leaves out its id as connections[%d] does: at most one element may leave it out, and that one is %s (R-STOR-050)", leftOut, DefaultConnectionID)
		case raw.ID == "":
			leftOut = i
		case !ValidConnectionID(raw.ID):
			r.Add(key, "id %q must match ^[a-z][a-z0-9-]{0,30}[a-z0-9]$ (R-STOR-050)", raw.ID)
		}
		if j, dup := seen[c.ID]; dup && !secondLeftOut {
			r.Add(key, "id %q is the id of connections[%d] as well; every id is unique (R-STOR-050)", c.ID, j)
		} else if !dup {
			seen[c.ID] = i
		}
		validateConnection(r, key, raw, c)
		if !c.Drain {
			nonDraining++
		}
	}
	if len(conns) > 0 && nonDraining == 0 {
		r.Add("connections", "every element has drain: true: one connection at least must take the uploads (R-STOR-050)")
	}
	if n := s.Replication.MinCopies; len(conns) > 0 && (n < 1 || n > nonDraining) {
		r.Add("replication.min_copies", "(%d) must be between 1 and the number of connections without drain: true (%d) (R-STOR-050)", n, nonDraining)
	}
	for _, issue := range locationClashes(conns, false) {
		r.Add(issue.key, "%s", issue.msg)
	}
	s.validateEncryptedPartSizes(r)
}

// validateConnection checks one element; raw is it as written, c with its defaults.
func validateConnection(r *Report, key string, raw, c StorageConnection) {
	exclusive := func(plain, plainVal, file, fileVal string) {
		if plainVal != "" && fileVal != "" {
			r.Add(key, "%s and %s exclude each other", plain, file)
		}
	}
	exclusive("bucket", raw.Bucket, "bucket_file", raw.BucketFile)
	exclusive("endpoint", raw.Endpoint, "endpoint_file", raw.EndpointFile)
	exclusive("region", raw.Region, "region_file", raw.RegionFile)
	checkKeyPrefix(r, key+" prefix", c.KeyPrefix())
	endpoint := NormalizeEndpoint(c.Endpoint)
	rejectUserinfo(r, key+" endpoint", endpoint, "the element's access_key_id_file")
	rejectUserinfo(r, key+" sts_endpoint", c.STSEndpoint, "")
	checkEndpointURL(r, key+" endpoint", endpoint, c.AllowInsecureEndpoint)
	oneOf := func(name, v string, allowed ...string) {
		for _, a := range allowed {
			if v == a {
				return
			}
		}
		r.Add(key, "%s %q must be one of %s", name, v, strings.Join(allowed, ", "))
	}
	oneOf("payload_checksum", c.PayloadChecksum, "sha256", "md5", "none")
	oneOf("request_checksum_calculation", c.RequestChecksumCalculation, "when_supported", "when_required")
	oneOf("response_checksum_validation", c.ResponseChecksumValidation, "when_supported", "when_required")
	oneOf("credentials_source", c.CredentialsSource, "default", "webIdentity", "static")
	oneOf("sse_mode", c.SSEMode, "none", "s3", "kms")
	if c.CredentialsSource == "static" && (c.AccessKeyIDFile == "" || c.SecretAccessKeyFile == "") {
		r.Add(key, "credentials_source static needs access_key_id_file and secret_access_key_file (R-STOR-031)")
	}
	if c.CredentialsSource == "webIdentity" && c.WebIdentityTokenFile == "" {
		r.Add(key, "web_identity_token_file is required with credentials_source webIdentity (R-STOR-031)")
	}
	if c.SSEMode == "kms" && c.SSEKMSKeyID == "" {
		r.Add(key, "sse_kms_key_id is required with sse_mode kms (R-STOR-032)")
	}
	if c.ScrubPeriod < minScrubPeriod {
		r.Add(key, "scrub_period (%s) must be at least %s (R-STOR-050)", c.ScrubPeriod, minScrubPeriod)
	}
	if c.ScrubBytesPerSecond < minScrubBytesPerSecond {
		r.Add(key, "scrub_bytes_per_second (%s) must be at least %s (R-STOR-050)", c.ScrubBytesPerSecond, minScrubBytesPerSecond)
	}
	if raw.KeyedNames != nil && !c.Encrypted {
		r.Add(key, "keyed_names is a detail of an encrypted connection; on a connection that is not encrypted the content is readable anyway (R-STOR-075)")
	}
	if _, err := parseCapacity(c.Capacity); err != nil {
		r.Add(key, "capacity %v (R-STOR-106)", err)
	}
}

// minCapacity is the least capacity an element may state (R-STOR-106, Q-STOR-503): a smaller one is a size written without its unit.
const minCapacity = GiB

// parseCapacity reads an element's capacity: empty states none (0), anything else is a size of at least minCapacity.
func parseCapacity(s string) (int64, error) {
	if strings.TrimSpace(s) == "" {
		return 0, nil
	}
	b, err := ParseByteSize(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a size (example: 5TiB)", s)
	}
	if b < minCapacity {
		return 0, fmt.Errorf("%q is below %s; write the size with its unit (example: 5TiB), or leave capacity empty to state none", s, minCapacity)
	}
	return int64(b), nil
}

// CapacityBytes is the element's capacity in bytes, the operator's statement of what the store may hold (R-STOR-106, Q-STOR-503); 0
// when it states none or the value does not pass the validation. Shown against the stored bytes, never enforced.
func (c StorageConnection) CapacityBytes() int64 {
	n, err := parseCapacity(c.Capacity)
	if err != nil {
		return 0
	}
	return n
}

// encryptedSegment is the plaintext of a segment of the stored format (storage.md R-STOR-076): while a connection is encrypted, a
// part holds whole segments (R-STOR-075).
const encryptedSegment = 64 * KiB

// validateEncryptedPartSizes refuses a part size that is not a multiple of 64 KiB while an element is encrypted (R-STOR-075 rule 2):
// storage.object_store.multipart.part_size cuts the parts of a write, storage.uploads.part_size those of an upload session.
func (s *Storage) validateEncryptedPartSizes(r *Report) {
	encrypted := ""
	for i, c := range s.Connections {
		if c.Encrypted {
			encrypted = fmt.Sprintf("connections[%d] (%s)", i, c.WithDefaults().ID)
			break
		}
	}
	if encrypted == "" {
		return
	}
	for _, p := range []struct {
		key  string
		size ByteSize
	}{{"object_store.multipart.part_size", s.ObjectStore.Multipart.PartSize}, {"uploads.part_size", s.Uploads.PartSize}} {
		if p.size%encryptedSegment != 0 {
			r.Add(p.key, "(%s) must be a multiple of 64KiB while %s is encrypted, so that a part holds whole segments (R-STOR-075)", p.size, encrypted)
		}
	}
}

// awsEndpoint is what an empty endpoint compares as: the AWS endpoint of the region (R-STOR-050 rule 4).
func awsEndpoint(region string) string {
	if region == "" {
		return "https://s3.amazonaws.com"
	}
	return "https://s3." + region + ".amazonaws.com"
}

// ComparableEndpoint is an endpoint as the location rule compares it (R-STOR-050 rule 4): with its scheme, without a
// trailing slash, lower case, and the AWS endpoint of its region when empty.
func ComparableEndpoint(endpoint, region string) string {
	e := NormalizeEndpoint(endpoint)
	if e == "" {
		e = awsEndpoint(region)
	}
	return strings.TrimSuffix(strings.ToLower(e), "/")
}

func comparableEndpoint(c StorageConnection) string { return ComparableEndpoint(c.Endpoint, c.Region) }

type locationIssue struct{ key, msg string }

// locationClashes lists the pairs of connections at one location (R-STOR-050 rule 4): equal endpoints (an empty one as the
// AWS endpoint of its region), equal buckets, and prefixes that are equal or of which one contains the other. With resolved
// false an element whose bucket, endpoint or region comes from a file is left out, because its value is not known before
// the file is read.
func locationClashes(conns []StorageConnection, resolved bool) []locationIssue {
	var out []locationIssue
	for i := range conns {
		a := conns[i].WithDefaults()
		if !resolved && (a.BucketFile != "" || a.EndpointFile != "" || a.RegionFile != "") {
			continue
		}
		for j := i + 1; j < len(conns); j++ {
			b := conns[j].WithDefaults()
			if !resolved && (b.BucketFile != "" || b.EndpointFile != "" || b.RegionFile != "") {
				continue
			}
			if a.Bucket == "" || a.Bucket != b.Bucket || comparableEndpoint(a) != comparableEndpoint(b) {
				continue
			}
			if prefixContains(a.KeyPrefix(), b.KeyPrefix()) || prefixContains(b.KeyPrefix(), a.KeyPrefix()) {
				out = append(out, locationIssue{
					key: connectionKey(j, b.ID),
					msg: fmt.Sprintf("lies at the location of connections[%d] (%s): bucket %s on one endpoint with the prefixes %q and %q, of which one contains the other; a location belongs to one connection (R-STOR-050)",
						i, a.ID, b.Bucket, a.KeyPrefix(), b.KeyPrefix()),
				})
			}
		}
	}
	return out
}

// ResolveConnections reads the files of every element (bucket_file, region_file, endpoint_file, R-STOR-050 rule 2), whose
// trimmed content becomes the value of the plain key, applies the defaults and the endpoint's scheme, and checks the location
// rule on the resolved values (rule 4). A file that cannot be read or is empty is an error naming the key and the connection.
// readFile is os.ReadFile when nil. It runs once at start, where the clients are built.
func ResolveConnections(conns []StorageConnection, readFile func(string) ([]byte, error)) ([]StorageConnection, error) {
	if readFile == nil {
		readFile = os.ReadFile
	}
	out := make([]StorageConnection, len(conns))
	var errs []error
	for i, raw := range conns {
		c := raw.WithDefaults()
		key := "storage." + connectionKey(i, c.ID)
		read := func(name, path string, dst *string) {
			if path == "" {
				return
			}
			b, err := readFile(path)
			switch {
			case err != nil:
				errs = append(errs, fmt.Errorf("%s %s %s cannot be read: %w", key, name, path, err))
			case strings.TrimSpace(string(b)) == "":
				errs = append(errs, fmt.Errorf("%s %s %s is empty", key, name, path))
			default:
				*dst = strings.TrimSpace(string(b))
			}
		}
		read("bucket_file", c.BucketFile, &c.Bucket)
		read("region_file", c.RegionFile, &c.Region)
		read("endpoint_file", c.EndpointFile, &c.Endpoint)
		c.Endpoint = NormalizeEndpoint(c.Endpoint)
		if c.EndpointFile != "" {
			var rep []Issue
			checkEndpointURL(&Report{prefix: "", issues: &rep}, key+" endpoint (from endpoint_file)", c.Endpoint, c.AllowInsecureEndpoint)
			for _, is := range rep {
				errs = append(errs, errors.New(is.Message))
			}
		}
		out[i] = c
	}
	for _, issue := range locationClashes(out, true) {
		errs = append(errs, fmt.Errorf("storage.%s %s", issue.key, issue.msg))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// FirstClient is the client configuration of the first connection with its files read: the one connection the operator
// commands, the backup and the restore reach until they learn of several (storage.md R-STOR-058, backup-and-restore.md
// R-BAK-030). A configuration without a connection is an error naming storage.connections.
func (s Storage) FirstClient() (StorageS3, error) {
	if len(s.Connections) == 0 {
		return StorageS3{}, errors.New("storage.connections: no connection is configured (R-STOR-050)")
	}
	conns, err := ResolveConnections(s.Connections, nil)
	if err != nil {
		return StorageS3{}, err
	}
	return ClientConfig(0, conns[0], s.ObjectStore), nil
}
