package main

import (
	"atom/commands"
	"atom/config"
	"atom/internal/daemon"
	"context"
	_ "embed"
	"fmt"
	"log"
	"os"

	"github.com/urfave/cli/v3"
)

//go:embed config.yaml
var DefaultConfigTemplate string

var (
	Version   = "Unknown"
	GitCommit = "Unknown"
)

func main() {
	cmd := &cli.Command{
		Name:                  "atom",
		Usage:                 "Decentralized mesh VPN node manager",
		EnableShellCompletion: true,
		Commands: []*cli.Command{
			{
				Name:  "version",
				Usage: "Show the atom version",
				Action: func(ctx context.Context, c *cli.Command) error {
					fmt.Printf("atom (rev. %s tag %s)\n", GitCommit, Version)
					return nil
				},
			},
			{
				Name:  "confgen",
				Usage: "Generate a documented default config file",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "output",
						Usage:   "Path to save the generated config file",
						Aliases: []string{"o"},
						Value:   "config.yaml",
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					outputPath := c.String("output")
					if err := os.WriteFile(outputPath, []byte(DefaultConfigTemplate), 0644); err != nil {
						return fmt.Errorf("failed to write config file: %w", err)
					}
					fmt.Printf("Successfully generated configuration file at %s\n", outputPath)
					return nil
				},
			},
			{
				Name:  "daemon",
				Usage: "Start the mesh node",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:      "config-path",
						Usage:     "Path to the configuration YAML file",
						Aliases:   []string{"c"},
						TakesFile: true,
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					configPath := c.String("config-path")
					if configPath == "" {
						return fmt.Errorf("required flag \"config-path\" not set")
					}

					cfg, err := config.LoadConfig(configPath)
					if err != nil {
						return fmt.Errorf("invalid config: %w", err)
					}

					if err := daemon.Start(*cfg); err != nil {
						return fmt.Errorf("daemon error: %w", err)
					}
					return nil
				},
				Commands: []*cli.Command{
					{
						Name:  "stop",
						Usage: "Gracefully stop the running daemon",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:      "socket",
								Usage:     "Path to the daemon unix socket",
								Aliases:   []string{"s"},
								TakesFile: true,
								Value:     "/var/run/atom.sock",
							},
						},
						Action: func(ctx context.Context, c *cli.Command) error {
							commands.StopDaemonCommand(c.String("socket"))
							return nil
						},
					},
				},
			},
			{
				Name:  "node",
				Usage: "Manage mesh nodes",
				Commands: []*cli.Command{
					{
						Name:  "list",
						Usage: "List all nodes currently in the mesh",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:      "socket",
								Usage:     "Path to the daemon unix socket",
								Aliases:   []string{"s"},
								TakesFile: true,
								Value:     "/var/run/atom.sock",
							},
							&cli.BoolFlag{
								Name:    "json",
								Usage:   "Output as JSON",
								Aliases: []string{"j"},
							},
						},
						Action: func(ctx context.Context, c *cli.Command) error {
							commands.ListNodesCommand(c.String("socket"), c.Bool("json"))
							return nil
						},
					},
					{
						Name:  "remove",
						Usage: "Remove a node from the mesh",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:      "socket",
								Usage:     "Path to the daemon unix socket",
								Aliases:   []string{"s"},
								TakesFile: true,
								Value:     "/var/run/atom.sock",
							},
						},
						Action: func(ctx context.Context, c *cli.Command) error {
							if c.Args().Len() < 1 {
								return fmt.Errorf("node name is required")
							}
							commands.RemoveNodeCommand(c.String("socket"), c.Args().First())
							return nil
						},
					},
				},
			},
			{
				Name:  "token",
				Usage: "Manage join tokens",
				Commands: []*cli.Command{
					{
						Name:  "create",
						Usage: "Generate a new join token",
						Flags: []cli.Flag{
							&cli.IntFlag{
								Name:    "uses",
								Usage:   "Number of times the token can be used",
								Aliases: []string{"u"},
								Value:   1,
							},
							&cli.StringFlag{
								Name:      "socket",
								Usage:     "Path to the daemon unix socket",
								Aliases:   []string{"s"},
								TakesFile: true,
								Value:     "/var/run/atom.sock",
							},
							&cli.BoolFlag{
								Name:    "json",
								Usage:   "Output as JSON",
								Aliases: []string{"j"},
							},
						},
						Action: func(ctx context.Context, c *cli.Command) error {
							uses := int(c.Int("uses"))
							socket := c.String("socket")

							commands.CreateTokenCommand(socket, uses, c.Bool("json"))
							return nil
						},
					},
					{
						Name:  "revoke",
						Usage: "Revoke an existing join token",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:      "socket",
								Usage:     "Path to the daemon unix socket",
								Aliases:   []string{"s"},
								TakesFile: true,
								Value:     "/var/run/atom.sock",
							},
						},
						Action: func(ctx context.Context, c *cli.Command) error {
							if c.Args().Len() < 1 {
								return fmt.Errorf("token ID is required")
							}
							commands.RevokeTokenCommand(c.String("socket"), c.Args().First())
							return nil
						},
					},
				},
			},
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
