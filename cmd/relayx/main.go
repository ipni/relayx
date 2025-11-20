package main

import (
	"fmt"
	"os"

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
				},
				Action: func(cctx *cli.Context) error {
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
					logger.Info("Stopping relayx server")
					return server.Stop()
				},
			},
		},
	}
	if err := app.Run(os.Args); err != nil {
		logger.Error("Error running app", "error", err)
		os.Exit(1)
	}
}
