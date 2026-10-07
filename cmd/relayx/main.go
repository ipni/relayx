package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	ppebble "github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/bloom"
	"github.com/ipfs/go-log/v2"
	"github.com/ipni/go-indexer-core"
	"github.com/ipni/go-indexer-core/store/pebble"
	"github.com/ipni/relayx"
	"github.com/urfave/cli/v2"
)

var logger = log.Logger("relayx/cmd")

func main() {
	ctx, stopSignalHandling := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignalHandling()

	app := cli.App{
		Name: "relayx",
		Commands: []*cli.Command{
			{
				Name:  "serve",
				Usage: "Start the relayx server",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "listen",
						Usage: "Address to listen on (default: :8080)",
						Value: "0.0.0.0:8080",
					},
					&cli.StringFlag{
						Name:     "delegate",
						Usage:    "The underlying indexer implementation to which to delegate requests. Supported values: pebble",
						Required: true,
					},
					&cli.PathFlag{
						Name:        "pebblePath",
						Usage:       "Data path of pebble database. Has no effect if --delegate is not pebble.",
						Value:       ".",
						DefaultText: "Current working directory",
					},
					&cli.PathFlag{
						Name:        "pebbleOptions",
						Usage:       "Path to the pebble options file. Has no effect if --delegate is not pebble.",
						DefaultText: "Default pebble options",
					},
					&cli.DurationFlag{
						Name:  "httpShutdownTimeout",
						Usage: "Maximum time to wait for in-flight HTTP requests on shutdown before closing remaining connections. Does not include subsequent indexer flush and close.",
						Value: 15 * time.Second,
					},
					&cli.BoolFlag{
						Name:  "meteringEnabled",
						Usage: "Enable the background per-provider metering scanner on the pebble delegate.",
					},
					&cli.IntFlag{
						Name:  "meteringBatchSize",
						Usage: "Maximum keys read per metering scan batch.",
						Value: 1000000,
					},
					&cli.DurationFlag{
						Name:  "meteringInterval",
						Usage: "Minimum time between automatic metering scans. Zero disables automatic scans (manual trigger still works).",
					},
					&cli.Float64Flag{
						Name:  "meteringTimeFill",
						Usage: "Fraction of time the metering scan spends reading, in (0, 1]. After a batch that took T, the scan sleeps T*(1-fill)/fill. 1 runs batches back to back.",
						Value: 0.1,
					},
					&cli.BoolFlag{
						Name:  "meteringExportProviderMetrics",
						Usage: "Export per-provider metering gauges to Prometheus. One series per provider; leave off unless the provider set is known to be small.",
					},
				},
				Action: func(cctx *cli.Context) error {
					httpShutdownTimeout := cctx.Duration("httpShutdownTimeout")
					if httpShutdownTimeout < 0 {
						return fmt.Errorf("httpShutdownTimeout must be >= 0, got %s", httpShutdownTimeout)
					}
					var delegate indexer.Interface
					switch d := cctx.String("delegate"); d {
					case "pebble":
						opts := &ppebble.Options{}
						opts.EnsureDefaults()
						if cctx.IsSet("pebbleOptions") {
							popts, err := os.ReadFile(cctx.Path("pebbleOptions"))
							if err != nil {
								return fmt.Errorf("failed to read pebble options file: %w", err)
							}
							if err := opts.Parse(string(popts), &ppebble.ParseHooks{
								NewFilterPolicy: func(name string) (ppebble.FilterPolicy, error) {
									switch name {
									case "none":
										return nil, nil
									case "rocksdb.BuiltinBloomFilter":
										return bloom.FilterPolicy(10), nil
									default:
										return nil, fmt.Errorf("unknown filter policy: %s", name)
									}
								},
								SkipUnknown: func(name, value string) bool {
									logger.Errorw("Unknown pebble option", "name", name, "value", value)
									return true
								},
							}); err != nil {
								return fmt.Errorf("failed to parse pebble options: %w", err)
							}
						}
						var pebbleOpts []pebble.Option
						if cctx.Bool("meteringEnabled") {
							pebbleOpts = append(pebbleOpts, pebble.WithMetering(pebble.MeteringConfig{
								BatchSize:             cctx.Int("meteringBatchSize"),
								Interval:              cctx.Duration("meteringInterval"),
								TimeFill:              cctx.Float64("meteringTimeFill"),
								ExportProviderMetrics: cctx.Bool("meteringExportProviderMetrics"),
							}))
						}
						var err error
						delegate, err = pebble.New(cctx.Path("pebblePath"), opts, pebbleOpts...)
						if err != nil {
							return fmt.Errorf("failed to create pebble indexer: %w", err)
						}
					default:
						return fmt.Errorf("unknown delegate: %s", d)
					}
					server, err := relayx.NewServer(
						relayx.WithListenAddr(cctx.String("listen")),
						relayx.WithDelegateIndexer(delegate),
						relayx.WithExportMeteringProviderMetrics(cctx.Bool("meteringExportProviderMetrics")))
					if err != nil {
						return err
					}
					if err := server.Start(); err != nil {
						return err
					}
					logger.Infow("Relayx server started", "address", cctx.String("listen"))
					<-cctx.Context.Done()
					// Restore default signal handling so a second SIGINT/SIGTERM
					// terminates immediately during a long flush/close.
					stopSignalHandling()
					return shutdown(server, delegate, httpShutdownTimeout)
				},
			},
		},
	}
	if err := app.RunContext(ctx, os.Args); err != nil {
		logger.Errorw("Error running app", "error", err)
		os.Exit(1)
	}
}

func shutdown(server *relayx.Server, delegate indexer.Interface, httpShutdownTimeout time.Duration) error {
	logger.Infow("Stopping HTTP server", "timeout", httpShutdownTimeout)
	httpCtx, httpCancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
	defer httpCancel()
	var errs error
	if err := server.StopContext(httpCtx); err != nil {
		logger.Errorw("HTTP server shutdown", "error", err)
		errs = errors.Join(errs, err)
	}

	logger.Info("Flushing delegate indexer")
	flushStart := time.Now()
	if err := delegate.Flush(); err != nil {
		logger.Errorw("Failed to flush delegate indexer", "error", err)
		errs = errors.Join(errs, err)
	} else {
		logger.Infow("Flushed delegate indexer", "took", time.Since(flushStart))
	}

	logger.Info("Closing delegate indexer")
	closeStart := time.Now()
	if err := delegate.Close(); err != nil {
		logger.Errorw("Failed to close delegate indexer", "error", err)
		errs = errors.Join(errs, err)
	} else {
		logger.Infow("Closed delegate indexer", "took", time.Since(closeStart))
	}
	return errs
}
