package other

import "log/slog"

func fail() { slog.Error("outside the formats") }
