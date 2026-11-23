//go:build integration

package api

import "testing"

// TestR_STOR_088_ADestroyedKeyInTheConnectionsList reads a destroyed storage key as the destruction leaves it in PostgreSQL (storage.md
// R-STOR-097) through GET of the connections (R-STOR-088, Q-API-405): its state and the time of its destruction, from the row through
// the connections' statement, summary.Rows and the handler, with no sealed value. One node, two rows.
func TestR_STOR_088_ADestroyedKeyInTheConnectionsList(t *testing.T) {
	e := newEnv(t, false)
	c := e.cluster(e.newNode("web-1", nodeOpts{}))
	e.exec(`INSERT INTO quader.storage_connections (id, bucket, prefix, encrypted, keyed_names, drained_at, retired_at)
		VALUES ('gone', 'gone', 'gone', true, true, '2027-03-01T09:00:00Z', '2027-03-02T09:00:00Z')`)
	e.exec(`INSERT INTO quader.storage_keys (key_id, connection_id, kind, generation, sealed_key, state, created_at, retired_at, destroyed_at)
		VALUES ('sk-gone-1-0a1b2c3d', 'gone', 'storage', 1, NULL, 'destroyed', '2026-11-30T08:00:00Z', '2027-03-02T09:00:00Z', '2027-04-02T09:00:00Z')`)
	t.Run("R-STOR-088 R-STOR-097 GET of a retired connection lists its destroyed key with destroyedAt", func(t *testing.T) {
		got := send(t, c, "GET", "/api/v1/system/storage-connections/gone", nil, e.reader.header()...)
		if got.Status != 200 {
			t.Fatalf("%d %s", got.Status, got.Body)
		}
		keys, _ := got.JSON(t)["storageKeys"].([]any)
		if len(keys) != 1 {
			t.Fatalf("storageKeys %s", got.Body)
		}
		k := keys[0].(map[string]any)
		if k["id"] != "sk-gone-1-0a1b2c3d" || k["state"] != "destroyed" || k["destroyedAt"] != "2027-04-02T09:00:00Z" {
			t.Fatalf("the destroyed key reads %v", k)
		}
	})
}
