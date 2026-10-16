package main

import (
	"reflect"
	"strings"
	"testing"

	"go.podman.io/image/v5/pkg/shortnames"
	"go.podman.io/image/v5/pkg/sysregistriesv2"
	"go.podman.io/image/v5/types"
)

// TestExpandShortNameMatchesUpstream guards the reimplementation against
// drift from pkg/shortnames (imported here only, so it stays out of the binary).
func TestExpandShortNameMatchesUpstream(t *testing.T) {
	disabled := types.ShortNameModeDisabled
	sys := &types.SystemContext{
		SystemRegistriesConfPath:    "testdata/registries.conf",
		SystemRegistriesConfDirPath: "testdata/registries.conf.d",
		UserShortNameAliasConfPath:  t.TempDir() + "/short-name-aliases.conf",
		ShortNameMode:               &disabled,
	}
	digest := "@sha256:" + strings.Repeat("b", 64)
	for _, input := range []string{
		"nginx", "nginx:1.27", "library/nginx", "nginx" + digest, "nginx:1.27" + digest,
		"fedora", "fedora:40", "fedora" + digest,
		"docker.io/alpine", "quay.io/a/b:c", "localhost/x", "host:5000/x" + digest,
		"UPPER", "", "a@b",
	} {
		sysregistriesv2.InvalidateCache()
		want, wantErr := shortnames.Resolve(sys, input)
		got, gotErr := expandShortName(sys, input)
		if (wantErr != nil) != (gotErr != nil) {
			t.Errorf("%q: error mismatch: upstream %v, ours %v", input, wantErr, gotErr)
			continue
		}
		if wantErr != nil {
			if wantErr.Error() != gotErr.Error() {
				t.Errorf("%q: error text: upstream %q, ours %q", input, wantErr, gotErr)
			}
			continue
		}
		var wantRefs, gotRefs []string
		for _, c := range want.PullCandidates {
			wantRefs = append(wantRefs, c.Value.String())
		}
		for _, r := range got.candidates {
			gotRefs = append(gotRefs, r.String())
		}
		if !reflect.DeepEqual(wantRefs, gotRefs) {
			t.Errorf("%q: upstream %q, ours %q", input, wantRefs, gotRefs)
		}
		if got.description != want.Description() {
			t.Errorf("%q: description: upstream %q, ours %q", input, want.Description(), got.description)
		}
	}
}
