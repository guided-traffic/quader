//go:build !drainfixture

package app

import "github.com/guided-traffic/quader/internal/httpserver"

// fixtureBuild is false in every release build.
const fixtureBuild = false

func registerFixture(*httpserver.Router) {}
