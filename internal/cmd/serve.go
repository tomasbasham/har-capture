package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/tomasbasham/cli-runtime/templates"

	"github.com/tomasbasham/har-capture/internal/capture"
	"github.com/tomasbasham/har-capture/internal/operation"
	"github.com/tomasbasham/har-capture/internal/server"
	"github.com/tomasbasham/har-capture/internal/storage"
)

type ServeOptions struct {
	uploader storage.Uploader

	Port              int
	GCSBucket         string
	NavigationTimeout time.Duration
	TotalTimeout      time.Duration
}

var (
	serveLong = templates.LongDesc(`Start the HAR capture HTTP server.`)

	serveExample = templates.Examples(`
		# Start on the default port
		har serve

		# Start on a custom port with a specific GCS bucket
		har serve --port 9090 --bucket my-har-bucket`)
)

func NewServeOptions() *ServeOptions {
	return &ServeOptions{}
}

func NewServeCommand(o *ServeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "serve",
		Short:   "Start the HAR capture HTTP server",
		Long:    serveLong,
		Example: serveExample,
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

	cmd.Flags().IntVarP(&o.Port, "port", "p", 8080, "Port to listen on")
	cmd.Flags().StringVarP(&o.GCSBucket, "bucket", "b", "", "GCS bucket name for artefact storage (required)")
	cmd.Flags().DurationVarP(&o.NavigationTimeout, "navigation-timeout", "n", 10*time.Second, "Default navigation timeout for captures")
	cmd.Flags().DurationVarP(&o.TotalTimeout, "total-timeout", "t", 30*time.Second, "Default total timeout for captures")

	return cmd
}

func (o *ServeOptions) Complete(cmd *cobra.Command, args []string) error {
	return nil
}

func (o *ServeOptions) Validate() error {
	return nil
}

func (o *ServeOptions) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var uploader storage.Uploader
	var err error

	if o.GCSBucket == "" {
		uploader, err = storage.NewGCSUploader(ctx, o.GCSBucket)
		if err != nil {
			return fmt.Errorf("failed to initialise GCS uploader: %w", err)
		}
	} else {
		path, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current working directory: %w", err)
		}
		uploader, err = storage.NewLocalUploader(path)
	}

	store := operation.NewMemoryStore()

	defaults := capture.Options{
		NavigationTimeout: o.NavigationTimeout,
		TotalTimeout:      o.TotalTimeout,
	}

	srv := server.New(store, uploader, defaults)

	addr := fmt.Sprintf(":%d", o.Port)
	fmt.Printf("Starting HAR capture server on %s\n", addr)
	return srv.ListenAndServe(addr)
}
