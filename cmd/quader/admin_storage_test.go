package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/blob/verify"
)

// TestR_HA_024_StorageVerifyRefusesWhatIsIncomplete: an unknown command, a malformed --since and a non-positive
// concurrency print the usage and open no connection.
func TestR_HA_024_StorageVerifyRefusesWhatIsIncomplete(t *testing.T) {
	s := streams{stdin: strings.NewReader(""), stdout: io.Discard, stderr: io.Discard}
	for _, args := range [][]string{
		{"storage"},
		{"storage", "frobnicate"},
		{"storage", "verify", "--since", "yesterday"},
		{"storage", "verify", "--concurrency", "0"},
	} {
		err := adminCmd(args, s)
		var u usageError
		if !errors.As(err, &u) || !strings.Contains(u.Error(), "quader admin storage verify") {
			t.Errorf("%v: err = %v, want the usage of quader admin storage verify", args, err)
		}
	}
	if !strings.Contains(adminUsage, "storage verify") {
		t.Error("the usage of quader admin does not list storage verify")
	}
}

// TestR_HA_025_StorageVerifyPrintsOneLinePerItem: the text report names each missing copy, the items of a lost blob, the
// totals per connection and the totals last, in the shape the end-to-end tests read.
func TestR_HA_025_StorageVerifyPrintsOneLinePerItem(t *testing.T) {
	var out bytes.Buffer
	rep := &verify.Report{
		Checked: 5, Lost: 1, Missing: 1, ItemsAffected: 3,
		Copies: []verify.CopyFinding{
			{Connection: "default", SHA256: "ab", Kind: verify.KindMissing, StoredSize: 3},
			{Connection: "secondary", SHA256: "ab", Kind: verify.KindMissing, StoredSize: 3},
			{Connection: "secondary", SHA256: "cd", Kind: verify.KindSizeMismatch, StoredSize: 3, ObjectSize: 1},
		},
		Findings: []verify.Finding{{
			Kind: verify.KindMissing, SHA256: "ab", Size: 3, ItemCount: 3,
			Items: []verify.Item{{Project: "p", Repository: "npm-local", Path: "a/b.tgz"}, {Project: "p", Repository: "npm-local", Path: "c.tgz", Trashed: true}},
		}},
		Connections: []verify.ConnectionReport{{Connection: "default", Checked: 5, Missing: 1}, {Connection: "secondary", Checked: 5, Missing: 1, SizeMismatch: 1}},
	}
	if err := writeVerifyReport(&out, rep); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"copy default ab: object missing",
		"copy secondary cd: object has 1 bytes, the row says 3",
		"item p/npm-local:a/b.tgz: no copy has its object, blob ab",
		"item p/npm-local:c.tgz (trashed): no copy has its object, blob ab",
		"blob ab: and 1 more items",
		"connection secondary: 5 copies checked, 1 missing, 1 of another size, 0 not checked",
		"5 blobs checked, 1 without object, 0 of another size, 0 not checked, 3 items affected",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, out.String())
		}
	}
}

// TestR_STOR_058_StorageVerifyExitsByLostBlobs: missing copies alone exit 0, a lost blob 5, and with --connection a finding of
// that connection 5; an unanswered check alone 75.
func TestR_STOR_058_StorageVerifyExitsByLostBlobs(t *testing.T) {
	code := func(err error) int {
		var ee *backup.ExitError
		if errors.As(err, &ee) {
			return ee.Code
		}
		if err != nil {
			return -1
		}
		return 0
	}
	oneMissing := &verify.Report{
		Copies:      []verify.CopyFinding{{Connection: "default", Kind: verify.KindMissing}},
		Connections: []verify.ConnectionReport{{Connection: "default", Missing: 1}, {Connection: "secondary"}},
	}
	for _, tc := range []struct {
		name string
		rep  *verify.Report
		conn string
		want int
	}{
		{"a copy missing on one of two connections", oneMissing, "", 0},
		{"the same with --connection default", oneMissing, "default", 5},
		{"the same with --connection secondary", oneMissing, "secondary", 0},
		{"a lost blob", &verify.Report{Lost: 1}, "", 5},
		{"an unanswered check", &verify.Report{Unchecked: []verify.Unchecked{{Connection: "default"}}}, "", 75},
	} {
		if got := code(verifyExit(tc.rep, nil, tc.conn)); got != tc.want {
			t.Errorf("%s: exit %d, want %d", tc.name, got, tc.want)
		}
	}
}
