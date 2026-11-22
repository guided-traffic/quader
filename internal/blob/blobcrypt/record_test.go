package blobcrypt

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"testing"
)

func TestR_STOR_077_ARecordRoundTripsAndStaysSmall(t *testing.T) {
	t.Parallel()
	sk := newStorageKey(t, strings.Repeat("k", 64)) // the longest identifier
	for _, size := range []int{0, 1, 65537} {
		o := newObject(t, sk, rule(size))
		total := 0
		for k, v := range o.meta {
			total += len(k) + len(v)
		}
		if len(o.meta) != 4 || total > 2048 || len(o.meta[MetaWrap]) != 155 || len(o.meta[MetaCommitment]) != 43 {
			t.Fatalf("the record of %d bytes is %d keys of %d bytes, wrap %d and commitment %d characters", size, len(o.meta), total,
				len(o.meta[MetaWrap]), len(o.meta[MetaCommitment]))
		}
		dk, err := Open(o.meta, holds(sk), o.want(o.stored))
		if err != nil || !dk.Key.Equal(o.dk.Key) || dk.Digest != o.dk.Digest || dk.Size != o.dk.Size || dk.KeyID != sk.ID {
			t.Fatalf("the record of %d bytes does not round-trip: %v", size, err)
		}
	}
}

// The steps run in their order: a record that fails several gets the reason of the earliest.
func TestR_STOR_077_TheStepsRunInTheirOrder(t *testing.T) {
	t.Parallel()
	sk := newStorageKey(t, "sk-default-1-0a1b2c3d")
	o := newObject(t, sk, rule(100))
	badWrap := b64().EncodeToString(flip(mustDecode(t, o.meta[MetaWrap]), 40))
	badCommit := b64().EncodeToString(flip(mustDecode(t, o.meta[MetaCommitment]), 1))
	wrongCopy := o.want(o.stored)
	wrongCopy.Digest[0] ^= 1
	wrongSize := o.want(o.stored)
	wrongSize.ObjectSize++
	unknown := withMeta(o.meta, MetaKey, "sk-default-9-99999999")
	lookupFailed := errors.New("the rows could not be read")
	for _, c := range []struct {
		name   string
		meta   map[string]string
		keys   KeyLookup
		want   Want
		reason Reason
	}{
		{"format before the key", withMeta(unknown, MetaFormat, "x"), holds(sk), o.want(o.stored), ReasonFormat},
		{"the key before the wrap", withMeta(unknown, MetaWrap, badWrap), holds(sk), o.want(o.stored), ReasonUnknownKey},
		{"the wrap before the copy", withMeta(o.meta, MetaWrap, badWrap), holds(sk), wrongCopy, ReasonRecord},
		{"the copy before the size", o.meta, holds(sk), Want{Known: true, StoredSize: 1, ObjectSize: 1}, ReasonRecord},
		{"the size before the commitment", withMeta(o.meta, MetaCommitment, badCommit), holds(sk), wrongSize, ReasonSize},
		{"the commitment last", withMeta(o.meta, MetaCommitment, badCommit), holds(sk), o.want(o.stored), ReasonCommitment},
	} {
		_, err := Open(c.meta, c.keys, c.want)
		if got, ok := ReasonOf(err); !ok || got != c.reason {
			t.Errorf("%s: %v, want %s", c.name, err, c.reason)
		}
	}
	// A failed lookup is no finding: it passes through and has no reason.
	_, err := Open(o.meta, func(string) (*StorageKey, error) { return nil, lookupFailed }, o.want(o.stored))
	if _, ok := ReasonOf(err); ok || !errors.Is(err, lookupFailed) {
		t.Errorf("a failed key lookup became %v", err)
	}
}

// No key, data key or seed reaches a formatted string or a log line.
func TestKeysRenderNothingOfThemselves(t *testing.T) {
	t.Parallel()
	raw := seq(0xa0, 32)
	k := mustKey(t, raw)
	sk, err := NewStorageKey("sk-default-1-0a1b2c3d", raw)
	if err != nil {
		t.Fatal(err)
	}
	dk := DataKey{Key: k, KeyID: sk.ID, Size: 7}
	var logged bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logged, nil))
	log.Info("keys", "key", k, "storage", sk, "data", dk)
	renderings := map[string]string{"the JSON log line": logged.String()}
	for i, v := range []any{k, sk, dk, *sk, &dk} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%x", "%X", "%d", "%q"} {
			renderings[fmt.Sprintf("value %d with %s", i, verb)] = fmt.Sprintf(verb, v)
		}
	}
	decimal := make([]string, 0, 5) // the form %d and %v give a []byte: "160 161 162 163 164"
	for _, b := range raw[:5] {
		decimal = append(decimal, strconv.Itoa(int(b)))
	}
	for what, text := range renderings {
		for _, leak := range []string{hex.EncodeToString(raw), strings.ToUpper(hex.EncodeToString(raw)), string(raw), strings.Join(decimal, " ")} {
			if strings.Contains(text, leak) {
				t.Errorf("%s holds the key's bytes", what) // the rendering itself is not printed: it holds the key
			}
		}
	}
}

func TestADestroyedKeyRefusesEveryUse(t *testing.T) {
	t.Parallel()
	k, err := NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	copyOfK := k
	k.Destroy()
	if _, err := copyOfK.Bytes(); !errors.Is(err, errDestroyed) {
		t.Error("a copy of a destroyed key still yields bytes")
	}
	if !bytes.Equal(copyOfK.m.b[:], make([]byte, KeySize)) {
		t.Error("Destroy left the key's bytes in memory")
	}
	if _, err := NewSection(copyOfK, [32]byte{}, nil, 0, 0, &Final{}); !errors.Is(err, errDestroyed) {
		t.Error("a destroyed key sealed a section")
	}
	if _, err := KeyedName(copyOfK, "", [32]byte{}); !errors.Is(err, errDestroyed) {
		t.Error("a destroyed key named an object")
	}
	sk := newStorageKey(t, "sk-default-1-0a1b2c3d")
	if _, err := Wrap(sk, DataKey{Key: copyOfK}, rand.Reader); !errors.Is(err, errDestroyed) {
		t.Error("a destroyed data key was wrapped")
	}
	if k.Equal(copyOfK) {
		t.Error("two destroyed keys compare equal")
	}
}

// Two connections with two name keys name one digest differently, and a name never passes for a digest's name (R-STOR-086).
func TestKeyedNamesDifferPerNameKey(t *testing.T) {
	t.Parallel()
	d := sha256.Sum256([]byte("artifact"))
	a, _ := KeyedName(mustKey(t, seq(1, 32)), "quader", d)
	b, _ := KeyedName(mustKey(t, seq(2, 32)), "quader", d)
	bare, _ := KeyedName(mustKey(t, seq(1, 32)), "", d)
	if a == b || !strings.HasPrefix(a, "quader/keyed/") || !strings.HasPrefix(bare, "keyed/") || strings.Contains(a, hex.EncodeToString(d[:])) {
		t.Errorf("keyed names %q, %q, %q", a, b, bare)
	}
}
