package main

import (
	"atom/commands"
	"atom/config"
	"atom/internal/daemon"
	"flag"
	"fmt"
	"log"
	"os"

	_ "github.com/hashicorp/raft"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	daemonCmd := flag.NewFlagSet("daemon", flag.ExitOnError)
	flag.NewFlagSet("tokens", flag.ExitOnError)

	switch os.Args[1] {
	case "daemon":
		configPath := daemonCmd.String("config-path", "", "Path to the configuration YAML file (required)")
		daemonCmd.Parse(os.Args[2:])
		if *configPath == "" {
			log.Fatal("--config-path parameter is required. Please specify the path to your config file.")
			os.Exit(3)
		}

		config, err := config.LoadConfig(*configPath)
		if err != nil {
			log.Fatalf("Invalid config - %s", err)
		}
		fmt.Printf("%+v\n", config)
		err = daemon.Start(*config)
		if err != nil {
			fmt.Printf("[error]: %s\n", err)
			os.Exit(1)
		}

	case "tokens":
		if len(os.Args) < 3 {
			printTokensUsage()
			os.Exit(1)
		}
		switch os.Args[2] {
		case "create":
			createCmd := flag.NewFlagSet("create", flag.ExitOnError)
			uses := createCmd.Int("uses", 1, "Number of times the token can be used")
			socket := createCmd.String("socket", "/var/run/atom.sock", "Path to the daemon unix socket")
			commands.CreateTokenCommand(*socket, *uses)
		default:
			fmt.Printf("Unknown tokens command: %s\n", os.Args[2])
			printTokensUsage()
			os.Exit(1)
		}

	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}

}

func printUsage() {
	fmt.Println("Usage: atom <command> [options]")
	fmt.Println("Commands:")
	fmt.Println("  daemon   Start the mesh node")
	fmt.Println("  tokens   Manage join tokens")
}

func printTokensUsage() {
	fmt.Println("Usage: atom tokens <command> [options]")
	fmt.Println("Commands:")
	fmt.Println("  create   Generate a new join token")
	fmt.Println("           --uses (default 1)")
	fmt.Println("           --socket (default /var/run/atom.sock)")
}
