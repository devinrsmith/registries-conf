package main

import (
	"fmt"
	"io"

	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/pkg/sysregistriesv2"
	"go.podman.io/image/v5/types"
)

type resolution struct {
	Input string `json:"input"`
	// ResolvedBy is "qualified", "alias" or "unqualified-search".
	ResolvedBy string `json:"resolvedBy"`
	// Origin is the config file the alias or unqualified-search registries
	// came from; empty for qualified inputs.
	Origin      string      `json:"origin,omitempty"`
	Description string      `json:"description,omitempty"`
	Candidates  []candidate `json:"candidates"`
}

type candidate struct {
	Reference string `json:"reference"`
	// Prefix of the matching [[registry]] table; empty when no table matched
	// and the reference is pulled as-is.
	RegistryPrefix string       `json:"registryPrefix,omitempty"`
	Blocked        bool         `json:"blocked"`
	PullSources    []pullSource `json:"pullSources"`
}

type pullSource struct {
	Reference string `json:"reference"`
	Location  string `json:"location"`
	Insecure  bool   `json:"insecure"`
	Mirror    bool   `json:"mirror"`
}

func runResolve(args []string, out io.Writer) error {
	fs, common := newFlagSet("resolve", "resolve [flags] IMAGE...", formatText, formatJSON, formatPlain)
	if err := common.parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return fmt.Errorf("resolve: at least one IMAGE is required")
	}
	sys := common.systemContext()

	var results []resolution
	for _, image := range fs.Args() {
		r, err := resolve(sys, image)
		if err != nil {
			return err
		}
		results = append(results, r)
	}

	switch common.format {
	case formatJSON:
		return writeJSON(out, results)
	case formatPlain:
		// Every pull source in the order a client would try them.
		for _, r := range results {
			for _, c := range r.Candidates {
				for _, s := range c.PullSources {
					fmt.Fprintln(out, s.Reference)
				}
			}
		}
	default:
		for _, r := range results {
			fmt.Fprintln(out, r.Input)
			if r.Description != "" {
				fmt.Fprintf(out, "  # %s\n", r.Description)
			}
			for _, c := range r.Candidates {
				fmt.Fprintf(out, "  %s\n", c.Reference)
				if c.Blocked {
					fmt.Fprintf(out, "    blocked by [[registry]] prefix %q\n", c.RegistryPrefix)
				}
				for _, s := range c.PullSources {
					kind := "primary"
					if s.Mirror {
						kind = "mirror"
					}
					insecure := ""
					if s.Insecure {
						insecure = " (insecure)"
					}
					fmt.Fprintf(out, "    %-8s%s%s\n", kind, s.Reference, insecure)
				}
			}
		}
	}
	return nil
}

// resolve expands image into its candidate references and, for each, the
// endpoints a containers/image client would try, in order.
func resolve(sys *types.SystemContext, image string) (resolution, error) {
	exp, err := expandShortName(sys, image)
	if err != nil {
		return resolution{}, err
	}
	res := resolution{Input: image, ResolvedBy: exp.resolvedBy, Origin: exp.origin, Description: exp.description}
	for _, ref := range exp.candidates {
		c, err := pullSources(sys, ref)
		if err != nil {
			return resolution{}, err
		}
		res.Candidates = append(res.Candidates, c)
	}
	return res, nil
}

func pullSources(sys *types.SystemContext, ref reference.Named) (candidate, error) {
	c := candidate{Reference: ref.String(), PullSources: []pullSource{}}
	reg, err := sysregistriesv2.FindRegistry(sys, ref.String())
	if err != nil {
		return candidate{}, fmt.Errorf("loading registries configuration: %w", err)
	}
	if reg == nil {
		// No [[registry]] table applies: the reference is pulled verbatim.
		c.PullSources = append(c.PullSources, pullSource{
			Reference: ref.String(),
			Location:  reference.Domain(ref),
		})
		return c, nil
	}
	c.RegistryPrefix = reg.Prefix
	if reg.Blocked {
		c.Blocked = true
		return c, nil
	}
	sources, err := reg.PullSourcesFromReference(ref)
	if err != nil {
		return candidate{}, fmt.Errorf("computing pull sources for %s: %w", ref, err)
	}
	// PullSourcesFromReference always appends the primary endpoint last.
	for i, s := range sources {
		c.PullSources = append(c.PullSources, pullSource{
			Reference: s.Reference.String(),
			Location:  s.Endpoint.Location,
			Insecure:  s.Endpoint.Insecure,
			Mirror:    i < len(sources)-1,
		})
	}
	return c, nil
}
