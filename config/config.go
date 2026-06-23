package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type NetworkLayerConfig struct {
	IP   string `yaml:"ip"`
	CIDR byte   `yaml:"cidr"`
}

type NodeConfig struct {
	Name         string             `yaml:"name"`
	Bootstrap    bool               `yaml:"bootstrap"`
	NetworkLayer NetworkLayerConfig `yaml:"network"`
}

type NetworkConfig struct {
	WireguardPort int    `yaml:"wireguard_port"`
	TCPJoinPort   uint16 `yaml:"tcp_join_port"`
}

type ConsensusConfig struct {
	DataDir  string `yaml:"data_dir"`
	RaftPort uint16 `yaml:"raft_port"`
}

type SecurityConfig struct {
	JoinToken      string `yaml:"join_token"`
	PrivateKeyPath string `yaml:"private_key_path"`
}

type ApiConfig struct {
	SocketPath      string `yaml:"socket_path"`
	InternalRPCPort uint16 `yaml:"internal_rpc_port"`
}

type JoinConfig struct {
	Endpoint string `yaml:"endpoint"`
}

type Config struct {
	Node      NodeConfig      `yaml:"node"`
	Network   NetworkConfig   `yaml:"network_config"`
	Join      JoinConfig      `yaml:"join_config"`
	Consensus ConsensusConfig `yaml:"consensus"`
	Security  SecurityConfig  `yaml:"security_config"`
	Api       ApiConfig       `yaml:"api_config"`
}

func LoadConfig(filePath string) (*Config, error) {

	fileBytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", filePath, err)
	}

	storage := Config{
		Node: NodeConfig{
			Bootstrap: false,
		},
		Network: NetworkConfig{
			WireguardPort: 51820,
			TCPJoinPort:   7700,
		},
		Consensus: ConsensusConfig{
			DataDir:  "/var/lib/atom/data",
			RaftPort: 7000,
		},
		Security: SecurityConfig{
			PrivateKeyPath: "/etc/atom/private.key",
		},
		Api: ApiConfig{
			SocketPath:      "/var/run/atom.sock",
			InternalRPCPort: 7001,
		},
	}

	if err := yaml.Unmarshal(fileBytes, &storage); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &storage, nil
}
