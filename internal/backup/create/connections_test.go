package create

import (
	"log/slog"
	"testing"

	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/backup/format"
)

// onBoth gives a blob of the env a present copy on connection secondary as well.
func (e *env) onBoth(d format.Digest) {
	e.src.second[d] = append([]byte(nil), e.src.data[d]...)
	e.src.rows[d] = []string{"default", "secondary"}
}

func (e *env) poolObject(d format.Digest) ([]byte, bool) {
	return e.mem.Data("quader/pool/sha256/" + d.Hex()[:2] + "/" + d.Hex()[2:4] + "/" + d.Hex())
}

// TestR_BAK_030_TheBackupReadsOneCopyPerBlob runs the copy phase over two connections with fakes (unit, 0.1 s, bytes of data).
func TestR_BAK_030_TheBackupReadsOneCopyPerBlob(t *testing.T) {
	t.Run("a blob the first connection lacks is copied from the second and is no damage", func(t *testing.T) {
		e := newEnv(t)
		d := e.addBlob("alpha")
		e.onBoth(d)
		delete(e.src.data, d) // deleted behind the first connection's back; its row still says present
		res, err := e.create(backup.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if res.DamagedBlobs != 0 || res.BlobsCopied != 1 || res.Outcome != "completed" {
			t.Fatalf("res = %+v", res)
		}
		if b, ok := e.poolObject(d); !ok || string(b) != "alpha" {
			t.Fatalf("pool object = %q %v", b, ok)
		}
		if res.SourceFailures["default"] != 1 || res.SourceFailures["secondary"] != 0 {
			t.Fatalf("sourceFailures = %v, want one on default", res.SourceFailures)
		}
	})
	t.Run("with the object gone from both connections the blob is damaged", func(t *testing.T) {
		e := newEnv(t)
		d := e.addBlob("alpha")
		e.onBoth(d)
		delete(e.src.data, d)
		delete(e.src.second, d)
		res, err := e.create(backup.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		m := e.manifest(res.BackupID)
		if res.DamagedBlobs != 1 || len(m.Blobs.Damaged) != 1 || m.Blobs.Damaged[0].Status != "missing" {
			t.Fatalf("res = %+v damaged = %+v", res, m.Blobs.Damaged)
		}
	})
	t.Run("a copy that does not hash to the digest moves the run to the next copy", func(t *testing.T) {
		e := newEnv(t)
		d := e.addBlob("alpha")
		e.onBoth(d)
		e.src.data[d] = []byte("alphb")
		e.cfg.Copy.ServerSide = "never"
		res, err := e.create(backup.CreateOptions{})
		if err != nil || res.DamagedBlobs != 0 {
			t.Fatalf("res = %+v err = %v", res, err)
		}
		if b, _ := e.poolObject(d); string(b) != "alpha" {
			t.Fatalf("pool object = %q", b)
		}
	})
	t.Run("an unavailable connection is passed over, and a blob only it holds ends the run temporarily", func(t *testing.T) {
		e := newEnv(t)
		both := e.addBlob("alpha")
		e.onBoth(both)
		e.src.down["default"] = true
		res, err := e.create(backup.CreateOptions{})
		if err != nil || res.DamagedBlobs != 0 || res.BlobsCopied != 1 {
			t.Fatalf("res = %+v err = %v", res, err)
		}

		e = newEnv(t)
		e.addBlob("beta") // only on default
		e.src.down["default"] = true
		_, err = e.create(backup.CreateOptions{})
		exitOf(t, err, backup.ExitTemporary, backup.CodeTemporary)
		if e.hasManifest() {
			t.Fatal("a run that could not read a blob wrote a manifest")
		}
	})
	t.Run("the copy on the target's endpoint goes first, a verified one first, a draining connection's last", func(t *testing.T) {
		c := &copier{r: &runState{d: Deps{Cfg: testConfig(), Log: slog.New(slog.DiscardHandler)}}, serverOff: map[string]bool{}}
		copies := []backup.SourceCopy{
			{Connection: "c", Position: 3},
			{Connection: "a", Position: 1, Drain: true},
			{Connection: "b", Position: 2},
			{Connection: "s1", Position: 4, ServerSide: true},
			{Connection: "s2", Position: 5, ServerSide: true, Verified: true},
		}
		got := ids(c.order(copies))
		if want := "s2 s1 b c a"; got != want {
			t.Fatalf("order = %s, want %s", got, want)
		}
		c.disableServer("s2")
		if got, want := ids(c.order(copies)), "s1 b c s2 a"; got != want {
			t.Fatalf("order after the target refused s2 = %s, want %s", got, want)
		}
		c.r.d.Cfg.Copy.ServerSide = "never"
		if got, want := ids(c.order(copies)), "b c s1 s2 a"; got != want {
			t.Fatalf("order with server_side never = %s, want %s", got, want)
		}
	})
}

func ids(copies []backup.SourceCopy) string {
	out := ""
	for i, c := range copies {
		if i > 0 {
			out += " "
		}
		out += c.Connection
	}
	return out
}
