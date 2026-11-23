package bad

import (
	"log"
	lg "log/slog"
)

func fail() {
	lg.Error("request failed")
	_ = lg.Default()
	log.Printf("x")
}
