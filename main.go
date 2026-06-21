package main

import (
	"atom/config"
	"flag"
	"fmt"
	"log"
	"os"

	_ "github.com/hashicorp/raft"
)

type Args struct {
	ConfigPath string
}

func initFlags() Args {
	configPath := flag.String("config-path", "", "Path to the configuration YAML file (required)")
	flag.Parse()

	if *configPath == "" {
		log.Fatal("--config-path parameter is required. Please specify the path to your config file.")
		os.Exit(3)
	}

	return Args{*configPath}
}

func main() {
	args := initFlags()

	config, err := config.LoadConfig(args.ConfigPath)

	if err != nil {
		log.Fatalf("Invalid config - %s", err)
	}

	fmt.Printf("%+v\n", config)
}
