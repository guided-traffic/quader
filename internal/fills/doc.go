// Package fills keeps the downloads from the upstreams of remote repositories (operations-and-observability.md R-OPS-049 to
// R-OPS-052, ADR-279): the tracker every remote engine of a replica reports to (proxy.DownloadTracker), the speed over a window of ten
// seconds, the publisher that writes the replica's running downloads and its ended ones to PostgreSQL every publish interval and sweeps
// the rows of replicas that stopped, and the store the API reads.
//
// Nothing here lies on the path of a fetch but an atomic addition per read of a body and a lock at the begin and the end: the writes are
// the publisher's, once per interval and replica, whatever the number of downloads (Q-OPS-420).
package fills
