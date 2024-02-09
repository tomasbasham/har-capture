package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/tomasbasham/cli-runtime/iooption"
	"github.com/tomasbasham/cli-runtime/templates"

	"github.com/tomasbasham/har-capture/internal/capture"
	"github.com/tomasbasham/har-capture/internal/storage"
)

type CaptureOptions struct {
	outFile *os.File

	URL               string
	NavigationTimeout time.Duration
	TotalTimeout      time.Duration
	OutPath           string

	iooption.IOStreams
}

var (
	captureLong = templates.LongDesc(``)

	captureExample = templates.Examples(``)
)

func NewCaptureOptions(streams iooption.IOStreams) *CaptureOptions {
	return &CaptureOptions{
		IOStreams: streams,
	}
}

func NewCaptureCommand(o *CaptureOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:                   "capture [URL]",
		DisableFlagsInUseLine: true,
		Short:                 "Capture a HAR file for the specified URL",
		Long:                  captureLong,
		Example:               captureExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Complete(cmd, args); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			if err := o.Run(); err != nil {
				return err
			}
			return nil
		},
	}

	// Add persistent config flags.
	pflags := cmd.PersistentFlags()

	pflags.DurationVarP(&o.NavigationTimeout, "navigation-timeout", "n", 10*time.Second, "Navigation timeout duration")
	pflags.DurationVarP(&o.TotalTimeout, "total-timeout", "t", 30*time.Second, "Total capture timeout duration")
	pflags.StringVarP(&o.OutPath, "out", "o", "", "Output file (default: stdout)")

	return cmd
}

func (o *CaptureOptions) Complete(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("URL is required")
	}
	o.URL = args[0]
	return nil
}

func (o *CaptureOptions) Validate() error {
	if len(o.URL) == 0 {
		return fmt.Errorf("URL is required")
	}

	// Setup output. If an output file is specified, create it.
	outFile := o.OutPath
	if outFile != "" {
		f, err := os.Create(outFile)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		o.outFile = f // store for later cleanup.
	}

	return nil
}

func (o *CaptureOptions) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if o.outFile != nil {
		defer o.outFile.Close()
	}

	fmt.Fprintf(o.Out, "Capturing HAR for %s...\n", o.URL)
	result, err := capture.Capture(ctx, capture.Options{
		URL:               o.URL,
		NavigationTimeout: o.NavigationTimeout,
		TotalTimeout:      o.TotalTimeout,
		Screenshots:       true,
	})
	if err != nil {
		return fmt.Errorf("capture failed: %w", err)
	}

	fmt.Fprintf(o.Out, "Capture complete: TTFB=%s, TimedOut=%t\n", result.TTFB, result.TimedOut)
	if result.TimedOut {
		fmt.Fprintln(o.ErrOut, "Capture timed out before networkIdle; HAR may be incomplete")
	}

	harJSON, err := json.MarshalIndent(result.HAR, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal HAR: %w", err)
	}

	if _, err := o.outFile.Write(harJSON); err != nil {
		return fmt.Errorf("failed to write HAR file: %w", err)
	}

	path, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}
	uploader, err := storage.NewLocalUploader(path)
	if err != nil {
		return fmt.Errorf("failed to initialise local uploader: %w", err)
	}

	for _, s := range result.Screenshots {
		fmt.Fprintf(o.Out, "Uploading screenshot captured at %s...\n", s.CapturedAt.Format(time.RFC3339))
		uploader.Upload(ctx, &storage.UploadRequest{
			ObjectName:  fmt.Sprintf("screenshot_%s.png", s.CapturedAt.Format("20060102_150405.000")),
			Content:     bytes.NewReader(s.PNG),
			ContentType: "image/png",
		})
	}

	return nil
}
