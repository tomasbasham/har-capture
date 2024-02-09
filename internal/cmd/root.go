package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	cliflag "github.com/tomasbasham/cli-runtime/flag"
	"github.com/tomasbasham/cli-runtime/iooption"
	"github.com/tomasbasham/cli-runtime/printer"
	"github.com/tomasbasham/cli-runtime/templates"

	_ "github.com/tomasbasham/har-capture/internal/capture"
)

var (
	rootLong = templates.LongDesc(``)

	rootExamples = templates.Examples(``)

	// Injected at build time using ldflags.
	version = ""
	commit  = ""
)

// HAROptions defines the options for the `har` command.
type HAROptions struct {
	iooption.IOStreams
}

// NewHarOptions provides an initialised HAROptions instance.
func NewHarOptions(streams iooption.IOStreams) *HAROptions {
	return &HAROptions{
		IOStreams: streams,
	}
}

// NewRootCommand creates the `har` command with default arguments.
func NewRootCommand() *cobra.Command {
	options := NewHarOptions(iooption.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	})

	return NewRootCommandWithArgs(options)
}

// NewRootCommandWithArgs creates the `har` command and its nested
// children.
func NewRootCommandWithArgs(o *HAROptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:                   "har [command]",
		Version:               versionInfo(),
		DisableFlagsInUseLine: true,
		Short:                 "HAR capture and analysis tool",
		Long:                  rootLong,
		Example:               rootExamples,
		SilenceErrors:         true,
		SilenceUsage:          true,
	}

	printerOpts := printer.WarningPrinterOptions{Color: true}
	printer := printer.NewWarningPrinter(o.ErrOut, printerOpts)
	cmd.SetGlobalNormalizationFunc(cliflag.WarnWordSepNormalizeFunc(printer))

	cmd.AddCommand(NewCaptureCommand(NewCaptureOptions(o.IOStreams)))
	cmd.AddCommand(NewServeCommand(NewServeOptions()))

	// The globlal normalisation function ensures that all flags specified meet
	// the desired format, changing users' input if necessary.
	cmd.SetGlobalNormalizationFunc(cliflag.WordSepNormalizeFunc())

	return cmd
}

func versionInfo() string {
	if version == "" {
		return ""
	}
	return fmt.Sprintf("%s (commit: %s)", version, commit)
}
