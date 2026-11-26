// Package dist is the main installation's web interface as the binary embeds it (R-ARCH-004): the committed placeholder
// index.html, and one directory per locale that `npm run build:embed` in web/ adds (dist/en/, dist/de/, …), which are ignored by
// git and picked up by release builds. It is a package of its own so that only the main installation's composition root imports
// it: the role bastion serves a bundle of its own (internal/bastion/gui), and the main installation's bytes never enter its
// closure (bastion.md R-BAS-116).
package dist

import (
	"embed"
	"io/fs"
)

//go:embed all:*
var files embed.FS

// FS is the build, rooted as webui.Options.Assets takes it: <locale>/index.html per locale, or the placeholder index.html alone.
func FS() fs.FS { return files }
