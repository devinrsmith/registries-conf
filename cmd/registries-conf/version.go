package main

import (
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
)

// Set at link time by package.nix, since Nix builds have neither a module
// version nor VCS information in their build info:
//
//	-X main.version=0.1.0 -X main.revision=<git commit>[-dirty]
var (
	version  string
	revision string
)

// reportedModules are the dependencies that decide how registries.conf is
// read: containers/image parses it, and containers/storage supplies the
// search paths.
var reportedModules = []string{"go.podman.io/image/v5", "go.podman.io/storage"}

type versionInfo struct {
	Version      string            `json:"version"`
	Revision     string            `json:"revision,omitempty"`
	Modified     bool              `json:"modified,omitempty"`
	Go           string            `json:"go"`
	Dependencies map[string]string `json:"dependencies"`
}

func currentVersion() versionInfo {
	bi, _ := debug.ReadBuildInfo()
	return newVersionInfo(version, revision, bi)
}

// newVersionInfo prefers the link-time values and falls back to what the Go
// toolchain recorded (a module version for go install, VCS settings for a
// build inside a git checkout).
func newVersionInfo(version, revision string, bi *debug.BuildInfo) versionInfo {
	info := versionInfo{Version: version, Go: runtime.Version(), Dependencies: map[string]string{}}
	if rev, dirty := strings.CutSuffix(revision, "-dirty"); rev != "" {
		info.Revision, info.Modified = rev, dirty
	}
	if bi != nil {
		info.Go = bi.GoVersion
		if info.Version == "" && bi.Main.Version != "(devel)" {
			info.Version = bi.Main.Version
		}
		if info.Revision == "" {
			for _, s := range bi.Settings {
				switch s.Key {
				case "vcs.revision":
					info.Revision = s.Value
				case "vcs.modified":
					info.Modified = s.Value == "true"
				}
			}
		}
		for _, dep := range bi.Deps {
			if !slices.Contains(reportedModules, dep.Path) {
				continue
			}
			v := dep.Version
			if r := dep.Replace; r != nil {
				v += " => " + strings.TrimSpace(r.Path+" "+r.Version)
			}
			info.Dependencies[dep.Path] = v
		}
	}
	if info.Version == "" {
		info.Version = "devel"
	}
	return info
}

func runVersion(args []string, out io.Writer) error {
	fs, common := newFormatFlagSet("version", "version [flags]", formatText, formatJSON)
	if err := common.parse(fs, args); err != nil {
		return err
	}
	info := currentVersion()
	if common.format == formatJSON {
		return writeJSON(out, info)
	}
	writeVersionText(out, info)
	return nil
}

func writeVersionText(out io.Writer, info versionInfo) {
	fmt.Fprintf(out, "registries-conf %s\n", info.Version)
	rows := [][2]string{}
	if info.Revision != "" {
		rev := info.Revision
		if info.Modified {
			rev += " (modified)"
		}
		rows = append(rows, [2]string{"revision", rev})
	}
	rows = append(rows, [2]string{"go", info.Go})
	for _, path := range reportedModules {
		if v, ok := info.Dependencies[path]; ok {
			rows = append(rows, [2]string{path, v})
		}
	}
	width := 0
	for _, r := range rows {
		width = max(width, len(r[0]))
	}
	for _, r := range rows {
		fmt.Fprintf(out, "  %-*s  %s\n", width, r[0], r[1])
	}
}
