package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"
)

func TestNewVersionInfo(t *testing.T) {
	deps := []*debug.Module{
		{Path: "go.podman.io/image/v5", Version: "v5.41.2"},
		{Path: "go.podman.io/storage", Version: "v1.64.1", Replace: &debug.Module{Path: "../storage"}},
		{Path: "github.com/BurntSushi/toml", Version: "v1.6.0"}, // not reported
	}
	wantDeps := map[string]string{
		"go.podman.io/image/v5": "v5.41.2",
		"go.podman.io/storage":  "v1.64.1 => ../storage",
	}
	vcs := []debug.BuildSetting{{Key: "vcs.revision", Value: "abc123"}, {Key: "vcs.modified", Value: "true"}}

	for _, tc := range []struct {
		name              string
		version, revision string
		bi                *debug.BuildInfo
		want              versionInfo
	}{{
		name:    "nix build: link-time values, no module version or VCS data",
		version: "0.1.0", revision: "def456-dirty",
		bi:   &debug.BuildInfo{GoVersion: "go1.26.8", Main: debug.Module{Version: "(devel)"}, Deps: deps},
		want: versionInfo{Version: "0.1.0", Revision: "def456", Modified: true, Go: "go1.26.8", Dependencies: wantDeps},
	}, {
		name: "go install @v0.2.0",
		bi:   &debug.BuildInfo{GoVersion: "go1.26.8", Main: debug.Module{Version: "v0.2.0"}, Deps: deps},
		want: versionInfo{Version: "v0.2.0", Go: "go1.26.8", Dependencies: wantDeps},
	}, {
		name: "go build in a checkout",
		bi:   &debug.BuildInfo{GoVersion: "go1.26.8", Main: debug.Module{Version: "(devel)"}, Deps: deps, Settings: vcs},
		want: versionInfo{Version: "devel", Revision: "abc123", Modified: true, Go: "go1.26.8", Dependencies: wantDeps},
	}, {
		name:     "link-time revision wins over VCS data",
		revision: "def456",
		bi:       &debug.BuildInfo{GoVersion: "go1.26.8", Main: debug.Module{Version: "(devel)"}, Settings: vcs},
		want:     versionInfo{Version: "devel", Revision: "def456", Go: "go1.26.8", Dependencies: map[string]string{}},
	}} {
		if got := newVersionInfo(tc.version, tc.revision, tc.bi); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", tc.name, got, tc.want)
		}
	}
}

func TestVersionCommand(t *testing.T) {
	// Test binaries carry real build info, including containers/image.
	var info versionInfo
	if err := json.Unmarshal([]byte(runCmd(t, "version", "--json")), &info); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(info.Dependencies["go.podman.io/image/v5"], "v5.") {
		t.Errorf("image/v5 version missing: %+v", info)
	}

	var text, flag bytes.Buffer
	if err := run([]string{"version"}, &text); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"--version"}, &flag); err != nil {
		t.Fatal(err)
	}
	if text.String() != flag.String() || !strings.Contains(strings.Join(strings.Fields(text.String()), " "), "go.podman.io/image/v5 v5.") {
		t.Errorf("version output:\n%s\n--version output:\n%s", text.String(), flag.String())
	}

	// version doesn't read configuration, so it doesn't take the location flags.
	if err := run([]string{"version", "--registries-conf", "x"}, &bytes.Buffer{}); err == nil {
		t.Error("version accepted --registries-conf")
	}
}
