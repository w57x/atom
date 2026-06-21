package daemon

import (
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"atom/config"
	"atom/internal/fsm"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb"
	"github.com/vishvananda/netlink"
	"golang.zx2c4.com/wireguard/wgctrl"
)

type Daemon struct {
	cfg      config.Config
	wgClient *wgctrl.Client
	fsm      *fsm.MeshFSM
	raftNode *raft.Raft
}

func Start(cfg config.Config) error {
	wgClient, err := wgctrl.New()
	if err != nil {
		return err
	}
	defer wgClient.Close()

	meshFSM := &fsm.MeshFSM{
		State: &fsm.MeshState{
			Nodes:  make(map[string]fsm.Node),
			Tokens: make(map[string]fsm.Token),
		},
		WGClient: wgClient,
	}

	var localIP netip.Addr
	if cfg.Node.Bootstrap {
		prefixStr := fmt.Sprintf("%s/%d", cfg.Node.NetworkLayer.IP, cfg.Node.NetworkLayer.CIDR)

		prefix, err := netip.ParsePrefix(prefixStr)
		if err != nil {
			panic(err)
		}

		networkIp := prefix.Masked().Addr()
		localIP = networkIp.Next()
	} else {
		assignedIP, err := joinMeshCluster(cfg)
		if err != nil {
			return fmt.Errorf("failed to join mesh: %w", err)
		}
		localIP = assignedIP
	}

	if err := ensureWireguardInterface(localIP, cfg.Network.WireguardPort); err != nil {
		return fmt.Errorf("failed to configure wg0 interface: %w", err)
	}

	slog.Info("created wg client.")
	raftNode, err := setupRaft(cfg, localIP, meshFSM)
	if err != nil {
		return err
	}

	d := &Daemon{
		cfg:      cfg,
		wgClient: wgClient,
		fsm:      meshFSM,
		raftNode: raftNode,
	}

	errChan := make(chan error, 2)

	go func() {
		addr := fmt.Sprintf("0.0.0.0:%d", cfg.Network.TCPJoinPort)
		errChan <- d.listenTCP(addr)
	}()

	go func() {
		errChan <- StartUnixRPC(cfg.Api.SocketPath, d.raftNode)
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errChan:
		return fmt.Errorf("daemon service failed: %w", err)
	case sig := <-sigChan:
		fmt.Printf("\nShutting down gracefully on %v signal...\n", sig)
		return d.raftNode.Shutdown().Error()
	}
}

func joinMeshCluster(cfg config.Config) (netip.Addr, error) {
	panic("unimplemented")
}

func ensureWireguardInterface(ip netip.Addr, port int) error {
	linkName := "wg0" // FIXME: should not be constant like that
	link, err := netlink.LinkByName(linkName)

	if err != nil {
		wgLink := &netlink.GenericLink{
			LinkAttrs: netlink.LinkAttrs{Name: linkName},
			LinkType:  "wireguard",
		}
		if err := netlink.LinkAdd(wgLink); err != nil {
			return fmt.Errorf("failed to add link %s: %w", linkName, err)
		}
		link = wgLink
	}

	addrStr := fmt.Sprintf("%s/32", ip.String())
	addr, err := netlink.ParseAddr(addrStr)
	if err != nil {
		return fmt.Errorf("failed to parse address %s: %w", addrStr, err)
	}

	if err := netlink.AddrReplace(link, addr); err != nil {
		return fmt.Errorf("failed to set address: %w", err)
	}

	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to set link up: %w", err)
	}

	return nil
}

func setupRaft(cfg config.Config, localIP netip.Addr, machine raft.FSM) (*raft.Raft, error) {
	if err := os.MkdirAll(cfg.Consensus.DataDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create data dir: %w", err)
	}

	raftCfg := raft.DefaultConfig()
	raftCfg.LocalID = raft.ServerID(cfg.Node.Name)
	dbPath := filepath.Join(cfg.Consensus.DataDir, "raft.db")
	store, err := raftboltdb.NewBoltStore(dbPath)

	if err != nil {
		return nil, fmt.Errorf("failed to create bolt store: %w", err)
	}

	snapshots, err := raft.NewFileSnapshotStore(cfg.Consensus.DataDir, 3, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot store: %w", err)
	}

	bindAddr := fmt.Sprintf("%s:%d", localIP.String(), cfg.Consensus.RaftPort)
	tcpAddr, err := net.ResolveTCPAddr("tcp", bindAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve raft addr: %w", err)
	}

	transport, err := raft.NewTCPTransport(bindAddr, tcpAddr, 3, 10*time.Second, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed to create raft transport: %w", err)
	}

	raftNode, err := raft.NewRaft(raftCfg, machine, store, store, snapshots, transport)
	if err != nil {
		return nil, fmt.Errorf("failed to init raft node: %w", err)
	}

	if cfg.Node.Bootstrap {
		hasState, err := raft.HasExistingState(store, store, snapshots)
		if err != nil {
			return nil, fmt.Errorf("failed to check existing state: %w", err)
		}

		if !hasState {
			configuration := raft.Configuration{
				Servers: []raft.Server{
					{
						ID:      raftCfg.LocalID,
						Address: transport.LocalAddr(),
					},
				},
			}
			if err := raftNode.BootstrapCluster(configuration).Error(); err != nil {
				return nil, fmt.Errorf("failed to bootstrap cluster: %w", err)
			}
		}
	}

	return raftNode, nil
}

func (d *Daemon) listenTCP(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()

	slog.Info("started join session", "tcp", addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go d.handleConnection(conn)
	}
}

func (d *Daemon) handleConnection(conn net.Conn) {
	defer conn.Close()
}
