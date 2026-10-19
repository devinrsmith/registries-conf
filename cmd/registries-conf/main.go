// Command registries-conf reads containers-registries.conf(5) the same way
// Podman, Buildah, Skopeo and CRI-O do, by delegating to the shared
// go.podman.io/image/v5/pkg/sysregistriesv2 package.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"go.podman.io/image/v5/types"
)

const usage = `Usage: registries-conf <command> [flags] [args]

Read containers-registries.conf(5) exactly as containers/image consumers
(Podman, Buildah, Skopeo, CRI-O) do, including registries.conf.d drop-ins,
the per-user config and $CONTAINERS_REGISTRIES_CONF.

Commands:
  resolve IMAGE...   Show the ordered pull sources (mirrors, then the primary
                     location) for each image, resolving short names first.
  config             Show the effective, merged configuration.
  check              Load the configuration and exit non-zero if it is invalid.
  version            Show the version of this tool and of containers/image.

Run 'registries-conf <command> -h' for the flags of a command.
`

// Output formats. Every command supports text and json; plain is for
// commands with a natural one-item-per-line form.
const (
	formatText  = "text"
	formatJSON  = "json"
	formatPlain = "plain"
)

// commonFlags holds the flags subcommands share. The location flags map onto
// the types.SystemContext fields that containers/image uses to locate config.
type commonFlags struct {
	conf    string
	confDir string
	root    string
	aliases string
	format  string
	formats []string
}

// register adds the configuration-location flags and the format flags.
func (c *commonFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&c.conf, "registries-conf", "", "path to registries.conf (default: same lookup as podman)")
	fs.StringVar(&c.confDir, "registries-conf-dir", "", "path to the registries.conf.d drop-in directory")
	fs.StringVar(&c.root, "root", "", "prefix for the implicit absolute paths (e.g. /etc/containers), for inspecting another root filesystem")
	fs.StringVar(&c.aliases, "short-name-aliases", "", "path to the machine-generated short-name-aliases.conf that podman records choices in (default: $XDG_CACHE_HOME/containers/short-name-aliases.conf, or /var/cache/containers/short-name-aliases.conf as root)")
	c.registerFormat(fs)
}

// registerFormat adds --format and --json, the only flags shared by every
// command.
func (c *commonFlags) registerFormat(fs *flag.FlagSet) {
	fs.StringVar(&c.format, "format", formatText, "output format: "+strings.Join(c.formats, ", "))
	fs.BoolFunc("json", "shorthand for -format json", func(string) error {
		c.format = formatJSON
		return nil
	})
}

// parse parses args and rejects formats the command does not support.
func (c *commonFlags) parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !slices.Contains(c.formats, c.format) {
		return fmt.Errorf("%s: unsupported format %q (want one of: %s)", fs.Name(), c.format, strings.Join(c.formats, ", "))
	}
	return nil
}

func (c *commonFlags) systemContext() *types.SystemContext {
	return &types.SystemContext{
		SystemRegistriesConfPath:     c.conf,
		SystemRegistriesConfDirPath:  c.confDir,
		RootForImplicitAbsolutePaths: c.root,
		UserShortNameAliasConfPath:   c.aliases,
	}
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintf(os.Stderr, "registries-conf: %v\n", err)
		}
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return flag.ErrHelp
	}
	cmd, args := args[0], args[1:]
	switch cmd {
	case "resolve":
		return runResolve(args, out)
	case "config":
		return runConfig(args, out)
	case "check":
		return runCheck(args, out)
	case "version":
		return runVersion(args, out)
	case "-version", "--version":
		writeVersionText(out, currentVersion())
		return nil
	case "-h", "-help", "--help", "help":
		fmt.Fprint(out, usage)
		return nil
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func newFlagSet(name, synopsis string, formats ...string) (*flag.FlagSet, *commonFlags) {
	fs, c := newBareFlagSet(name, synopsis, formats)
	c.register(fs)
	return fs, c
}

// newFormatFlagSet is newFlagSet for commands that don't read configuration.
func newFormatFlagSet(name, synopsis string, formats ...string) (*flag.FlagSet, *commonFlags) {
	fs, c := newBareFlagSet(name, synopsis, formats)
	c.registerFormat(fs)
	return fs, c
}

func newBareFlagSet(name, synopsis string, formats []string) (*flag.FlagSet, *commonFlags) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: registries-conf %s\n\nFlags:\n", synopsis)
		fs.PrintDefaults()
	}
	return fs, &commonFlags{formats: formats}
}

func writeJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
