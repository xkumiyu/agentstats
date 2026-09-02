package version

import (
	"runtime/debug"
	"testing"
)

func TestSelectPrefersLinkedVersion(t *testing.T) {
	buildInfo := &debug.BuildInfo{Main: debug.Module{Version: "v0.9.0"}}
	if got := resolve("v1.0.0", buildInfo); got != "v1.0.0" {
		t.Fatalf("select linked version = %q", got)
	}
}

func TestSelectUsesModuleVersion(t *testing.T) {
	buildInfo := &debug.BuildInfo{Main: debug.Module{Version: "v1.0.0"}}
	if got := resolve(Development, buildInfo); got != "v1.0.0" {
		t.Fatalf("select module version = %q", got)
	}
}

func TestSelectFallsBackToDevelopment(t *testing.T) {
	for _, buildInfo := range []*debug.BuildInfo{
		nil,
		{Main: debug.Module{Version: ""}},
		{Main: debug.Module{Version: "(devel)"}},
	} {
		if got := resolve(Development, buildInfo); got != Development {
			t.Fatalf("select development version = %q", got)
		}
	}
}
