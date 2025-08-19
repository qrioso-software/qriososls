package assets

import "embed"

//go:embed templates/*.tmpl.yml
var Templates embed.FS
