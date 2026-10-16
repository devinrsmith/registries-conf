package main

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"go.podman.io/image/v5/pkg/sysregistriesv2"
	"go.podman.io/image/v5/types"
)

type effectiveConfig struct {
	// Sources are the registries.conf files and then the drop-in directories
	// that are searched, each list highest priority first. Not all need exist.
	Sources                     []string   `json:"sources"`
	ShortNameMode               string     `json:"shortNameMode"`
	UnqualifiedSearchRegistries []string   `json:"unqualifiedSearchRegistries"`
	CredentialHelpers           []string   `json:"credentialHelpers"`
	Registries                  []registry `json:"registries"`
}

type registry struct {
	Prefix             string     `json:"prefix"`
	Location           string     `json:"location"`
	Insecure           bool       `json:"insecure"`
	Blocked            bool       `json:"blocked"`
	MirrorByDigestOnly bool       `json:"mirrorByDigestOnly"`
	Mirrors            []endpoint `json:"mirrors"`
}

type endpoint struct {
	Location       string `json:"location"`
	Insecure       bool   `json:"insecure"`
	PullFromMirror string `json:"pullFromMirror,omitempty"`
}

func loadConfig(sys *types.SystemContext) (effectiveConfig, error) {
	sources, err := configSources(sys)
	if err != nil {
		return effectiveConfig{}, err
	}
	cfg := effectiveConfig{Sources: sources}

	// GetRegistries is deprecated only in favour of per-reference lookups;
	// it remains the sole way to enumerate every [[registry]] table.
	regs, err := sysregistriesv2.GetRegistries(sys) //nolint:staticcheck
	if err != nil {
		return cfg, err
	}
	mode, err := sysregistriesv2.GetShortNameMode(sys)
	if err != nil {
		return cfg, err
	}
	cfg.ShortNameMode = shortNameModeString(mode)
	if cfg.UnqualifiedSearchRegistries, err = sysregistriesv2.UnqualifiedSearchRegistries(sys); err != nil {
		return cfg, err
	}
	if cfg.CredentialHelpers, err = sysregistriesv2.CredentialHelpers(sys); err != nil {
		return cfg, err
	}

	cfg.Registries = []registry{}
	for _, r := range regs {
		out := registry{
			Prefix:             r.Prefix,
			Location:           r.Location,
			Insecure:           r.Insecure,
			Blocked:            r.Blocked,
			MirrorByDigestOnly: r.MirrorByDigestOnly,
			Mirrors:            []endpoint{},
		}
		for _, m := range r.Mirrors {
			out.Mirrors = append(out.Mirrors, endpoint{Location: m.Location, Insecure: m.Insecure, PullFromMirror: m.PullFromMirror})
		}
		cfg.Registries = append(cfg.Registries, out)
	}
	return cfg, nil
}

func shortNameModeString(m types.ShortNameMode) string {
	switch m {
	case types.ShortNameModeDisabled:
		return "disabled"
	case types.ShortNameModePermissive:
		return "permissive"
	case types.ShortNameModeEnforcing:
		return "enforcing"
	default:
		return "invalid"
	}
}

func runConfig(args []string, out io.Writer) error {
	fs, common := newFlagSet("config", "config [flags]", formatText, formatJSON)
	if err := common.parse(fs, args); err != nil {
		return err
	}
	cfg, err := loadConfig(common.systemContext())
	if err != nil {
		return err
	}
	if common.format == formatJSON {
		return writeJSON(out, cfg)
	}
	fmt.Fprintln(out, "sources:")
	for _, s := range cfg.Sources {
		fmt.Fprintf(out, "  %s\n", s)
	}
	fmt.Fprintf(out, "short-name-mode:               %s\n", cfg.ShortNameMode)
	fmt.Fprintf(out, "unqualified-search-registries: %s\n", strings.Join(cfg.UnqualifiedSearchRegistries, ", "))
	fmt.Fprintf(out, "credential-helpers:            %s\n", strings.Join(cfg.CredentialHelpers, ", "))
	for _, r := range cfg.Registries {
		fmt.Fprintf(out, "\n[[registry]] prefix=%q location=%q", r.Prefix, r.Location)
		if r.Insecure {
			fmt.Fprint(out, " insecure")
		}
		if r.Blocked {
			fmt.Fprint(out, " blocked")
		}
		if r.MirrorByDigestOnly {
			fmt.Fprint(out, " mirror-by-digest-only")
		}
		fmt.Fprintln(out)
		for _, m := range r.Mirrors {
			fmt.Fprintf(out, "  mirror %s", m.Location)
			if m.Insecure {
				fmt.Fprint(out, " insecure")
			}
			if m.PullFromMirror != "" {
				fmt.Fprintf(out, " pull-from-mirror=%s", m.PullFromMirror)
			}
			fmt.Fprintln(out)
		}
	}
	return nil
}

func runCheck(args []string, out io.Writer) error {
	fs, common := newFlagSet("check", "check [flags]", formatText, formatJSON)
	if err := common.parse(fs, args); err != nil {
		return err
	}
	cfg, err := loadConfig(common.systemContext())
	if err != nil {
		return err
	}
	if common.format != formatJSON {
		fmt.Fprintln(out, "ok, searched:")
		for _, s := range cfg.Sources {
			fmt.Fprintf(out, "  %s\n", s)
		}
		return nil
	}
	return writeJSON(out, map[string]any{"ok": true, "sources": cfg.Sources})
}

// configSources lists the paths sysregistriesv2 searches. The package only
// exposes them pre-formatted with %q for error messages, so parse that back
// rather than re-deriving the search order (rootless UIDs, overrides, ...)
// and risking drift from what podman actually reads.
func configSources(sys *types.SystemContext) ([]string, error) {
	desc := sysregistriesv2.ConfigurationSourceDescription(sys)
	if desc == "" {
		return nil, errors.New("cannot determine registries.conf search paths")
	}
	return parseQuotedList(desc)
}

// parseQuotedList inverts fmt.Sprintf("%q", []string{...}).
func parseQuotedList(s string) ([]string, error) {
	rest, ok := strings.CutPrefix(s, "[")
	if ok {
		rest, ok = strings.CutSuffix(rest, "]")
	}
	if !ok {
		return nil, fmt.Errorf("unexpected configuration source description %q", s)
	}
	list := []string{}
	for rest = strings.TrimLeft(rest, " "); rest != ""; rest = strings.TrimLeft(rest, " ") {
		quoted, err := strconv.QuotedPrefix(rest)
		if err != nil {
			return nil, fmt.Errorf("unexpected configuration source description %q: %w", s, err)
		}
		item, err := strconv.Unquote(quoted)
		if err != nil {
			return nil, err
		}
		list = append(list, item)
		rest = rest[len(quoted):]
	}
	return list, nil
}
