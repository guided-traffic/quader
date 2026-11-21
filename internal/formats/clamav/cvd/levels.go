package cvd

import (
	"strconv"
	"strings"
)

// release is one row of ClamAV's page "Versions and Functionality Levels" (docs.clamav.net, appendix FunctionalityLevels, read on
// 2026-11-21): a release and the functionality level of its engine.
type release struct {
	version string
	level   int
}

// releases are the rows of that page from 0.95 on, in version order. The level does not fall from one release to the next in that
// order (a patch of an older feature line stays below the next line's first release), so the first release whose level is at least
// a container's is the oldest release that loads it. A newer ClamAV adds rows here; until then a higher level reads "Newer than".
func releases() []release {
	return []release{
		{"0.95", 41},
		{"0.95.1", 42},
		{"0.95.2", 43},
		{"0.95.3", 44},
		{"0.96", 51},
		{"0.96.1", 53},
		{"0.96.2", 54},
		{"0.96.3", 55},
		{"0.96.4", 56},
		{"0.96.5", 58},
		{"0.97.0", 60},
		{"0.97.1", 61},
		{"0.97.2", 62},
		{"0.97.3", 63},
		{"0.97.4", 64},
		{"0.97.5", 65},
		{"0.97.6", 67},
		{"0.97.7", 68},
		{"0.97.8", 69},
		{"0.98.0", 74},
		{"0.98.1", 76},
		{"0.98.2", 77},
		{"0.98.3", 77},
		{"0.98.4", 77},
		{"0.98.5", 79},
		{"0.98.6", 79},
		{"0.98.7", 80},
		{"0.99.0", 81},
		{"0.99.1", 82},
		{"0.99.2", 82},
		{"0.99.3", 84},
		{"0.99.4", 85},
		{"0.100.0", 90},
		{"0.100.1", 92},
		{"0.100.2", 93},
		{"0.101.0", 100},
		{"0.101.1", 102},
		{"0.101.2", 102},
		{"0.101.3", 102},
		{"0.101.4", 105},
		{"0.101.5", 106},
		{"0.102.0", 110},
		{"0.102.1", 112},
		{"0.102.2", 113},
		{"0.102.3", 114},
		{"0.102.4", 115},
		{"0.103.0", 120},
		{"0.103.1", 122},
		{"0.103.2", 123},
		{"0.103.3", 124},
		{"0.103.4", 125},
		{"0.103.5", 126},
		{"0.103.6", 127},
		{"0.103.7", 128},
		{"0.103.8", 129},
		{"0.103.9", 130},
		{"0.103.10", 131},
		{"0.103.11", 132},
		{"0.103.12", 133},
		{"0.104.0", 140},
		{"0.104.1", 141},
		{"0.104.2", 142},
		{"0.104.3", 143},
		{"0.104.4", 144},
		{"0.105.0", 150},
		{"0.105.1", 151},
		{"0.105.2", 152},
		{"1.0.0", 160},
		{"1.0.1", 161},
		{"1.0.2", 162},
		{"1.0.3", 163},
		{"1.0.4", 164},
		{"1.0.5", 165},
		{"1.0.6", 166},
		{"1.0.7", 167},
		{"1.0.8", 168},
		{"1.0.9", 169},
		{"1.1.0", 180},
		{"1.1.1", 181},
		{"1.1.2", 182},
		{"1.1.3", 183},
		{"1.2.0", 190},
		{"1.2.1", 191},
		{"1.2.2", 192},
		{"1.2.3", 193},
		{"1.3.0", 200},
		{"1.3.1", 201},
		{"1.3.2", 202},
		{"1.4.0", 210},
		{"1.4.1", 211},
		{"1.4.2", 212},
		{"1.4.3", 213},
		{"1.4.4", 214},
		{"1.4.5", 215},
		{"1.4.6", 216},
		{"1.4.7", 217},
		{"1.5.0", 230},
		{"1.5.1", 231},
		{"1.5.2", 232},
		{"1.5.3", 233},
		{"1.5.4", 234},
		{"1.5.5", 235},
		{"1.6.0", 240},
	}
}

// Level is what a functionality level means for a ClamAV installation (R-CLAMAV-003).
type Level struct {
	// Release is the oldest release whose engine meets the level, written without a patch of 0 ("0.103", "1.0.9"); for a level above
	// the table's newest entry, that newest release.
	Release string
	// Newer is true for a level above the table's newest entry.
	Newer bool
}

// LevelOf reads a functionality level against the table.
func LevelOf(level int) Level {
	rs := releases()
	for _, r := range rs {
		if r.level >= level {
			return Level{Release: short(r.version)}
		}
	}
	return Level{Release: short(rs[len(rs)-1].version), Newer: true}
}

// Text is the level as the screens say it: "ClamAV 0.103 or later", or "Newer than ClamAV 1.6".
func (l Level) Text() string {
	if l.Newer {
		return "Newer than ClamAV " + l.Release
	}
	return "ClamAV " + l.Release + " or later"
}

// NewestLevel is the functionality level of the table's newest release.
func NewestLevel() int {
	rs := releases()
	return rs[len(rs)-1].level
}

// short drops a patch of 0: 0.103.0 is 0.103.
func short(v string) string {
	if p := strings.Split(v, "."); len(p) == 3 {
		if n, err := strconv.Atoi(p[2]); err == nil && n == 0 {
			return p[0] + "." + p[1]
		}
	}
	return v
}
