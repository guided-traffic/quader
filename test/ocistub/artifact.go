package ocistub

// Artifacts beside the generated corpus: an OCI manifest of one config and one layer, each of a media type the test names, the shape
// in which a publisher ships a file through a registry (the image scanning's database, vulnerability-scanning.md R-VULN-044, is one
// layer of application/vnd.aquasec.trivy.db.layer.v1.tar+gzip). A test adds one to a stub that serves nothing else with Empty and
// PutArtifact; the counts, the modes and the failures apply to it as to the corpus.

// Empty is a corpus without repositories: a stub that serves only what a test adds to it starts without the cost of Default.
func Empty() *Corpus { return newCorpus() }

// PutArtifact adds name:tag as an OCI image manifest of the config and the one layer, and returns the digests of the manifest and the
// layer. Adding the same tag again moves it to the new manifest; the earlier manifest and its blobs stay, served by their digests.
func (s *Server) PutArtifact(name, tag, configType string, config []byte, layerType string, layer []byte) (manifest, layerDigest string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.c.repos[name]
	if r == nil {
		r = newRepoDef()
		s.c.repos[name] = r
	}
	cd := s.c.addBlob(r, config)
	ld := s.c.addBlob(r, layer)
	o := s.c.addManifest(r, MediaOCIManifest, map[string]any{
		"schemaVersion": 2, "mediaType": MediaOCIManifest,
		"config": Descriptor{MediaType: configType, Digest: cd, Size: int64(len(config))},
		"layers": []Descriptor{{MediaType: layerType, Digest: ld, Size: int64(len(layer))}},
	})
	r.tags[tag] = o.digest
	return o.digest, ld
}
