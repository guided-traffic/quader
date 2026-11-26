// Package cataloguebounds holds the bounds of the public catalogue's transfer between the main installation and a Bastion
// (R-BAS-101): the page of the change list, the batch read and the largest document. They are figures, not the interface's
// shape, so that a later measurement can move them without a raise of the interface version (Q-BAS-345): the main
// installation clamps a request's limit to the page, answers a batch up to its bounds and builds no document above the
// largest, and a Bastion asks for at most the page and the batch and reads at most the largest. Both sides of one release
// carry the same figures; a Bastion of another release that asks for more gets the main installation's page and More, or the
// part of its batch the main installation answered.
//
// The figures follow the measurement of the catalogue's size of 2026-11-26 (docs/developer/bastion.md, "The size of the
// public catalogue"): 37,376 documents for 2,670 public versions, 45 MiB gzip, the largest document 560 KiB raw.
//
// The package imports nothing, so that both sides may import it (R-BAS-001).
package cataloguebounds

const (
	// ChangesPage is the number of entries the change list answers when the request names no limit.
	ChangesPage = 5000
	// ChangesPageMax is the most entries one answer of the change list holds; a larger limit is clamped to it.
	ChangesPageMax = 10000
	// DocumentMaxBytes is the largest document of the catalogue, raw: the main installation builds none larger, and a Bastion
	// decompresses none beyond it.
	DocumentMaxBytes = 16 << 20
	// BatchMaxPaths is the most paths one batch read names.
	BatchMaxPaths = 5000
	// BatchMaxBytes bounds the compressed bytes of one answer of the batch read: the main installation stops before the
	// frame that would pass it, and answers at least one frame.
	BatchMaxBytes = 8 << 20
)
