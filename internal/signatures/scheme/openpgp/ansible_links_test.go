package openpgp

import "testing"

// R-SIG-031, formats/ansible.md Q-ANS-91: a collection's file that is a link is checked by the digest of its target, which FILES.json
// lists for it as ansible-galaxy collection build writes it; a link FILES.json does not name, and one whose target is no file of the
// archive, break the chain.
func TestR_SIG_031_ALinkOfACollectionIsCheckedByItsTargetsDigest(t *testing.T) {
	sum := sha256Hex([]byte("echo remote\n"))
	file := func(name, digest string) collectionFile {
		return collectionFile{Name: name, Type: "file", ChecksumType: "sha256", SHA256: digest}
	}
	listed := collectionFiles{Files: []collectionFile{file("remote.sh", sum), file("aix.sh", sum)}}
	if err := filesMatch(listed, map[string]string{"remote.sh": sum}, map[string]string{"aix.sh": "remote.sh"}); err != nil {
		t.Errorf("a link listed with its target's digest: %v", err)
	}
	if err := filesMatch(collectionFiles{Files: listed.Files[:1]}, map[string]string{"remote.sh": sum}, map[string]string{"aix.sh": "remote.sh"}); err == nil {
		t.Error("a link FILES.json does not name was accepted")
	}
	if err := filesMatch(listed, map[string]string{"remote.sh": sum}, map[string]string{"aix.sh": "gone.sh"}); err == nil {
		t.Error("a link to no file of the archive was accepted")
	}
}
