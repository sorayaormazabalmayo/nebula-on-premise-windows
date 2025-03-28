package cli

import (
	"context"
	"flag"
	"log"

	"github.com/peterbourgon/ff/v4"
	"github.com/sorayaormazabalmayo/general-service/internal/server"
)

// NewGeneralServiceCommand creates and returns the root CLI command.
func NewGeneralServiceCommand(logger *log.Logger) ff.Command {
	fs := ff.NewFlagSet("general-service")

	return ff.Command{
		Name:      "general-service",
		ShortHelp: "This is the root command for the general-service",
		Usage:     "general-service [FLAGS] <SUBCOMMANDS> ...",
		Flags:     fs,
		Exec: func(context.Context, []string) error {
			return flag.ErrHelp
		},
		Subcommands: []*ff.Command{
			newServeCommand(logger),
		},
	}
}

// newServeCommand returns a usable ff.Command for the serve subcommand.
func newServeCommand(logger *log.Logger) *ff.Command {
	// Configuration structure
	cfg := &server.Config{}

	fs := ff.NewFlagSet("serve")
	_ = fs.String(0, "config", "", "config file in yaml format")
	fs.StringVar(&cfg.HTTPAddr, 0, "http-addr", "localhost:8000", "HTTP address")
	fs.StringVar(&cfg.InternatHTTPAddr, 0, "internal-http-addr", "localhost:9000", "Internal HTTP address")
	fs.BoolVarDefault(&cfg.Debug, 0, "debug", false, "Enable debug")
	fs.BoolVarDefault(&cfg.AutoUpdate, 0, "auto-update", false, "Enable updater")
	fs.StringVar(&cfg.MetadataURL, 0, "metadata-url", "https://sorayaormazabalmayo.github.io/TUF_Repository_YubiKey_Vault/metadata", "Metadata URL")

	cmd := &ff.Command{
		Name:      "serve",
		ShortHelp: "This SERVE subcommand starts general-service launching an HTTP server",
		Flags:     fs,
		Exec: func(_ context.Context, args []string) error {

			logger.Printf(
				"General server started: httpAddr: %s, internal-httpAddr: %s, debug: %v",
				cfg.HTTPAddr,
				cfg.InternatHTTPAddr,
				cfg.Debug,
			)

			// Start server
			s, err := server.NewServer(cfg, logger)
			if err != nil {
				return err
			}

			return s.Run(logger)
		},
	}
	return cmd
}
