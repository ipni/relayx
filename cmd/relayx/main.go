package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	ppebble "github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/bloom"
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
				},
				Action: func(cctx *cli.Context) error {
					httpShutdownTimeout := cctx.Duration("httpShutdownTimeout")
					if httpShutdownTimeout < 0 {
						return fmt.Errorf("httpShutdownTimeout must be >= 0, got %s", httpShutdownTimeout)
					}
					var delegate indexer.Interface
					switch d := cctx.String("delegate"); d {
					case "pebble":
						opts := (&ppebble.Options{}).EnsureDefaults()
						if cctx.IsSet("pebbleOptions") {
							popts, err := os.ReadFile(cctx.Path("pebbleOptions"))
							if err != nil {
								return fmt.Errorf("failed to read pebble options file: %w", err)
							}
							if err := opts.Parse(string(popts), &ppebble.ParseHooks{
								NewCache: func(size int64) *ppebble.Cache {
									return ppebble.NewCache(size)
								},
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
							}); err != nil {
								return fmt.Errorf("failed to parse pebble options: %w", err)
							}
						}
						var err error
						delegate, err = pebble.New(cctx.Path("pebblePath"), opts)
						if err != nil {
							return fmt.Errorf("failed to create pebble indexer: %w", err)
						}
					default:
						return fmt.Errorf("unknown delegate: %s", d)
					}
					server, err := relayx.NewServer(
						relayx.WithListenAddr(cctx.String("listen")),
						relayx.WithDelegateIndexer(delegate))
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
