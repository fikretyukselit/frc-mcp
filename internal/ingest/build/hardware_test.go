package build

import (
	"strings"
	"testing"

	"github.com/fikretyukselit/frc-mcp/internal/hwdata"
	"github.com/fikretyukselit/frc-mcp/internal/index"
)

func TestApplyHardware(t *testing.T) {
	row := func(part, source, season string) index.HWSpec {
		return index.HWSpec{Part: part, Source: source, Season: season}
	}
	curated := []hwdata.Row{
		{Shard: "hardware", Spec: row("sdsmk4i", "sds", "all")},
		{Shard: "hardware-restricted", Spec: row("x", "vendor", "all")},
	}
	shards := map[string]*shardData{"hardware": {name: "hardware", hwspecs: []index.HWSpec{row("neo", "wpilib-dcmotor", "2026")}}}
	w, s, err := applyHardware(shards, curated)
	if err != nil || w != 1 || s != 1 || len(shards["hardware"].hwspecs) != 2 {
		t.Fatalf("written %d skipped %d err %v rows %+v", w, s, err, shards["hardware"].hwspecs)
	}

	// The same (part, source, season) twice would be replaced silently by
	// INSERT OR REPLACE; the build refuses it instead.
	dup := map[string]*shardData{"hardware": {name: "hardware", hwspecs: []index.HWSpec{row("sdsmk4i", "sds", "all")}}}
	if _, _, err := applyHardware(dup, curated[:1]); err == nil || !strings.Contains(err.Error(), "two rows") {
		t.Fatalf("err = %v, want a duplicate-row error", err)
	}
}
