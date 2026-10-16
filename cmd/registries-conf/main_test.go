package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.podman.io/image/v5/pkg/sysregistriesv2"
	"go.podman.io/image/v5/types"
)

var testConf = []string{
	"--registries-conf", "testdata/registries.conf",
	"--registries-conf-dir", "testdata/registries.conf.d",
}

func TestMain(m *testing.M) {
	// Keep the user's real recorded aliases (and an unwritable $HOME, as in
	// the Nix sandbox) out of the tests.
	dir, err := os.MkdirTemp("", "registries-conf-test")
	if err != nil {
		panic(err)
	}
	testConf = append(testConf, "--short-name-aliases", filepath.Join(dir, "short-name-aliases.conf"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func runCmd(t *testing.T, args ...string) string {
	t.Helper()
	// containers/image caches parsed config per SystemContext paths.
	sysregistriesv2.InvalidateCache()
	var out bytes.Buffer
	if err := run(args, &out); err != nil {
		t.Fatalf("run(%v): %v", args, err)
	}
	return out.String()
}

func TestResolvePlain(t *testing.T) {
	digest := "@sha256:" + strings.Repeat("a", 64)
	for _, tc := range []struct {
		image string
		want  []string
	}{
		{"nginx", []string{
			"quay.io/nginx:latest",
			"mirror.internal:5000/dockerhub/library/nginx:latest",
			"registry-1.docker.io/library/nginx:latest",
		}},
		// digest-only mirrors join the list only for digest pulls.
		{"docker.io/library/nginx" + digest, []string{
			"mirror.internal:5000/dockerhub/library/nginx" + digest,
			"pinned.internal/library/nginx" + digest,
			"registry-1.docker.io/library/nginx" + digest,
		}},
		{"fedora:40", []string{"registry.fedoraproject.org/fedora:40"}},
		{"ghcr.io/a/b", []string{"ghcr.io/a/b:latest"}},
		{"evil.example.com/x", nil},
	} {
		args := append([]string{"resolve", "--format", "plain"}, testConf...)
		got := strings.Fields(runCmd(t, append(args, tc.image)...))
		if len(got) == 0 {
			got = nil
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got %q\nwant %q", tc.image, got, tc.want)
		}
	}
}

func TestResolveJSONBlocked(t *testing.T) {
	var res []resolution
	args := append([]string{"resolve", "--format", "json"}, testConf...)
	if err := json.Unmarshal([]byte(runCmd(t, append(args, "evil.example.com/x")...)), &res); err != nil {
		t.Fatal(err)
	}
	c := res[0].Candidates[0]
	if !c.Blocked || c.RegistryPrefix != "evil.example.com" || len(c.PullSources) != 0 {
		t.Errorf("unexpected candidate: %+v", c)
	}
}

func TestConfigJSON(t *testing.T) {
	var cfg effectiveConfig
	if err := json.Unmarshal([]byte(runCmd(t, append([]string{"config", "--json"}, testConf...)...)), &cfg); err != nil {
		t.Fatal(err)
	}
	if want := []string{"testdata/registries.conf", "testdata/registries.conf.d"}; !reflect.DeepEqual(cfg.Sources, want) {
		t.Errorf("sources = %q, want %q", cfg.Sources, want)
	}
	if cfg.ShortNameMode != "enforcing" {
		t.Errorf("shortNameMode = %q", cfg.ShortNameMode)
	}
	if want := []string{"quay.io", "docker.io"}; !reflect.DeepEqual(cfg.UnqualifiedSearchRegistries, want) {
		t.Errorf("unqualifiedSearchRegistries = %q", cfg.UnqualifiedSearchRegistries)
	}
	if len(cfg.Registries) != 2 || len(cfg.Registries[0].Mirrors) != 2 || !cfg.Registries[0].Mirrors[0].Insecure {
		t.Errorf("registries = %+v", cfg.Registries)
	}
}

func TestCheckInvalid(t *testing.T) {
	sysregistriesv2.InvalidateCache()
	dir := t.TempDir()
	err := run([]string{"check", "--registries-conf", "testdata/registries.conf", "--registries-conf-dir", dir + "/missing",
		"--root", dir}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	sysregistriesv2.InvalidateCache()
	bad := dir + "/bad.conf"
	writeFile(t, bad, `short-name-mode = "bogus"`)
	if err := run([]string{"check", "--registries-conf", bad, "--registries-conf-dir", dir + "/missing"}, &bytes.Buffer{}); err == nil {
		t.Fatal("invalid config accepted")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFormat(t *testing.T) {
	// --json is shorthand for --format json, and the last one given wins.
	for _, args := range [][]string{{"config", "--json"}, {"config", "--format", "text", "--json"}} {
		var cfg effectiveConfig
		if err := json.Unmarshal([]byte(runCmd(t, append(args, testConf...)...)), &cfg); err != nil {
			t.Errorf("%v: not JSON: %v", args, err)
		}
	}
	for _, args := range [][]string{
		{"config", "--format", "plain"},
		{"check", "--format", "plain"},
		{"resolve", "--format", "yaml", "nginx"},
	} {
		sysregistriesv2.InvalidateCache()
		err := run(append(args, testConf...), &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "unsupported format") {
			t.Errorf("%v: got %v, want unsupported format error", args, err)
		}
	}
}

func TestResolvedBy(t *testing.T) {
	// Record an alias the way podman does after a prompt, in a private file.
	recorded := filepath.Join(t.TempDir(), "short-name-aliases.conf")
	sys := &types.SystemContext{
		SystemRegistriesConfPath:    "testdata/registries.conf",
		SystemRegistriesConfDirPath: "testdata/registries.conf.d",
		UserShortNameAliasConfPath:  recorded,
	}
	if err := sysregistriesv2.AddShortNameAlias(sys, "busybox", "docker.io/library/busybox"); err != nil {
		t.Fatal(err)
	}
	conf := []string{
		"--registries-conf", "testdata/registries.conf",
		"--registries-conf-dir", "testdata/registries.conf.d",
		"--short-name-aliases", recorded,
	}

	for _, tc := range []struct {
		input, resolvedBy, origin string
	}{
		{"ghcr.io/a/b", "qualified", ""},
		{"fedora:40", "alias", "testdata/registries.conf.d/10-aliases.conf"},
		{"busybox", "alias", recorded},
		{"nginx", "unqualified-search", "testdata/registries.conf"},
	} {
		var res []resolution
		args := append([]string{"resolve", "--format", "json"}, conf...)
		if err := json.Unmarshal([]byte(runCmd(t, append(args, tc.input)...)), &res); err != nil {
			t.Fatal(err)
		}
		if res[0].ResolvedBy != tc.resolvedBy || res[0].Origin != tc.origin {
			t.Errorf("%s: resolvedBy=%q origin=%q, want %q %q", tc.input, res[0].ResolvedBy, res[0].Origin, tc.resolvedBy, tc.origin)
		}
	}
}

func TestParseQuotedList(t *testing.T) {
	for _, want := range [][]string{
		{},
		{"/etc/containers/registries.conf"},
		{"/a b/c", `/with "quotes"`, `/back\slash`, "/brackets]/[x", "/ünï/çødé", "/tab\there"},
	} {
		// The same formatting sysregistriesv2.ConfigurationSourceDescription uses.
		got, err := parseQuotedList(fmt.Sprintf("%q", want))
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("round trip of %q: got %q, %v", want, got, err)
		}
	}
	for _, bad := range []string{"", "/etc/containers/registries.conf", `["unterminated]`, `[unquoted]`} {
		if got, err := parseQuotedList(bad); err == nil {
			t.Errorf("parseQuotedList(%q) = %q, want error", bad, got)
		}
	}
}

func TestNamespaceProxy(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "registries.conf")
	writeFile(t, conf, `
[[registry]]
prefix = "registry.k8s.io"
location = "registry.k8s.io"
[[registry.mirror]]
location = "proxy.internal"
namespace-proxy = "registry.k8s.io"

[[registry]]
prefix = "quay.io"
location = "proxy.internal"
namespace-proxy = "quay.io"
`)
	flags := []string{
		"--registries-conf", conf,
		"--registries-conf-dir", filepath.Join(dir, "missing"),
		"--short-name-aliases", filepath.Join(dir, "short-name-aliases.conf"),
	}

	var cfg effectiveConfig
	if err := json.Unmarshal([]byte(runCmd(t, append([]string{"config", "--format", "json"}, flags...)...)), &cfg); err != nil {
		t.Fatal(err)
	}
	// The library orders [[registry]] tables itself, so look them up by prefix.
	byPrefix := map[string]registry{}
	for _, r := range cfg.Registries {
		byPrefix[r.Prefix] = r
	}
	if m := byPrefix["registry.k8s.io"].Mirrors; len(m) != 1 || m[0].NamespaceProxy != "registry.k8s.io" {
		t.Errorf("config registry.k8s.io mirrors = %+v", m)
	}
	if got := byPrefix["quay.io"].NamespaceProxy; got != "quay.io" {
		t.Errorf("config quay.io namespaceProxy = %q", got)
	}

	var res []resolution
	args := append([]string{"resolve", "--format", "json"}, flags...)
	if err := json.Unmarshal([]byte(runCmd(t, append(args, "registry.k8s.io/pause", "quay.io/a/b")...)), &res); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range res {
		for _, s := range r.Candidates[0].PullSources {
			got = append(got, s.Location+"="+s.NamespaceProxy)
		}
	}
	// The k8s primary has no proxy; the quay.io primary does.
	if want := []string{"proxy.internal=registry.k8s.io", "registry.k8s.io=", "proxy.internal=quay.io"}; !reflect.DeepEqual(got, want) {
		t.Errorf("resolve pull sources = %q, want %q", got, want)
	}

	text := runCmd(t, append([]string{"resolve"}, append(flags, "registry.k8s.io/pause")...)...)
	if !strings.Contains(text, "proxy.internal/pause:latest (namespace-proxy=registry.k8s.io)") {
		t.Errorf("text output lacks namespace-proxy:\n%s", text)
	}
}
