package main

import (
	"fmt"
	"strings"

	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/pkg/sysregistriesv2"
	"go.podman.io/image/v5/types"
)

// How an input was turned into candidate references.
const (
	resolvedQualified         = "qualified"          // input named a registry; no short-name resolution
	resolvedAlias             = "alias"              // short name matched an alias
	resolvedUnqualifiedSearch = "unqualified-search" // short name combined with each unqualified-search registry
)

type expansion struct {
	candidates  []reference.Named
	resolvedBy  string
	origin      string // config file(s) the alias or search registries came from
	description string // as pkg/shortnames' Resolved.Description
}

// expandShortName mirrors the non-interactive path of
// go.podman.io/image/v5/pkg/shortnames.Resolve with short-name-mode
// "disabled": an alias wins, otherwise every unqualified-search registry is a
// candidate.
//
// It is reimplemented rather than imported because pkg/shortnames links
// promptui, whose text/template dependency forces the linker to keep every
// exported method in the binary (roughly 2 MiB) for a prompt we never show.
// Keep it in step with upstream; the tests cover each branch.
func expandShortName(sys *types.SystemContext, input string) (expansion, error) {
	ref, err := reference.Parse(input)
	if err != nil {
		return expansion{}, fmt.Errorf("cannot parse input: %q: %w", input, err)
	}
	named, ok := ref.(reference.Named)
	if !ok {
		return expansion{}, fmt.Errorf("%q is not a named reference", input)
	}

	if registry := reference.Domain(named); strings.ContainsAny(registry, ".:") || registry == "localhost" {
		// Fully qualified; normalize docker.io/alpine to docker.io/library/alpine.
		named, err = reference.ParseNormalizedNamed(input)
		if err != nil {
			return expansion{}, fmt.Errorf("cannot normalize input: %q: %w", input, err)
		}
		return expansion{
			candidates: []reference.Named{reference.TagNameOnly(named)},
			resolvedBy: resolvedQualified,
		}, nil
	}

	// Look up aliases by repository only; the tag or digest is re-applied.
	repo := reference.TrimNamed(named)
	alias, aliasOrigin, err := sysregistriesv2.ResolveShortNameAlias(sys, repo.String())
	if err != nil {
		return expansion{}, err
	}
	if alias != nil {
		if tagged, ok := named.(reference.NamedTagged); ok {
			if alias, err = reference.WithTag(alias, tagged.Tag()); err != nil {
				return expansion{}, err
			}
		}
		if digested, ok := named.(reference.Digested); ok {
			if alias, err = reference.WithDigest(alias, digested.Digest()); err != nil {
				return expansion{}, err
			}
		}
		return expansion{
			candidates:  []reference.Named{reference.TagNameOnly(alias)},
			resolvedBy:  resolvedAlias,
			origin:      aliasOrigin,
			description: fmt.Sprintf("Resolved %q as an alias (%s)", repo.String(), aliasOrigin),
		}, nil
	}

	registries, origin, err := sysregistriesv2.UnqualifiedSearchRegistriesWithOrigin(sys)
	if err != nil {
		return expansion{}, err
	}
	if len(registries) == 0 {
		if origin != "" {
			return expansion{}, fmt.Errorf("short-name %q did not resolve to an alias and no unqualified-search registries are defined in %q", input, origin)
		}
		return expansion{}, fmt.Errorf("short-name %q did not resolve to an alias and no containers-registries.conf(5) was found", input)
	}
	exp := expansion{
		resolvedBy:  resolvedUnqualifiedSearch,
		origin:      origin,
		description: fmt.Sprintf("Resolving %q using unqualified-search registries (%s)", repo.String(), origin),
	}
	for _, reg := range registries {
		c, err := reference.ParseNormalizedNamed(reg + "/" + input)
		if err != nil {
			return expansion{}, fmt.Errorf("creating reference with unqualified-search registry %q: %w", reg, err)
		}
		exp.candidates = append(exp.candidates, reference.TagNameOnly(c))
	}
	return exp, nil
}
