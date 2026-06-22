package daemon

import (
	"bytes"
	"crypto/rand"
	"encoding/gob"
	"encoding/json"
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
	"atom/internal/network"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb"
	"github.com/vishvananda/netlink"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type Daemon struct {
	cfg      config.Config
	wgClient *wgctrl.Client
	fsm      *fsm.MeshFSM
	raftNode *raft.Raft
}

func loadOrGeneratePrivateKey(path string) (wgtypes.Key, error) {
	keyBytes, err := os.ReadFile(path)
	if err == nil {
		return wgtypes.ParseKey(string(bytes.TrimSpace(keyBytes)))
	}

	if os.IsNotExist(err) {
		slog.Info("Generating new WireGuard private key", "path", path)
		newKey, err := wgtypes.GeneratePrivateKey()
		if err != nil {
			return wgtypes.Key{}, err
		}

		if err := os.WriteFile(path, []byte(newKey.String()+"\n"), 0600); err != nil {
			return wgtypes.Key{}, fmt.Errorf("failed to save private key: %w", err)
		}
		return newKey, nil
	}

	return wgtypes.Key{}, err
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

	privKey, err := loadOrGeneratePrivateKey(cfg.Security.PrivateKeyPath)
	if err != nil {
		return fmt.Errorf("failed to setup private key: %w", err)
	}
	pubKey := privKey.PublicKey().String()

	var joinAccept *network.JoinAcceptPayload
	if cfg.Node.Bootstrap {
		prefixStr := fmt.Sprintf("%s/%d", cfg.Node.NetworkLayer.IP, cfg.Node.NetworkLayer.CIDR)

		prefix, err := netip.ParsePrefix(prefixStr)
		if err != nil {
			panic(err)
		}

		networkIp := prefix.Masked().Addr()
		localIP = networkIp.Next()
	} else {
		statePath := filepath.Join(cfg.Consensus.DataDir, "state.json")
		b, err := os.ReadFile(statePath)
		if err == nil {
			var savedState network.JoinAcceptPayload
			if err := json.Unmarshal(b, &savedState); err == nil {
				joinAccept = &savedState
				ip, _ := netip.ParseAddr(savedState.AssignedIP)
				localIP = ip
				slog.Info("Loaded existing node state from disk, skipping join handshake")
			}
		}

		if joinAccept == nil {
			acceptMsg, err := joinMeshCluster(cfg, pubKey)
			if err != nil {
				return fmt.Errorf("failed to join mesh: %w", err)
			}

			ip, _ := netip.ParseAddr(acceptMsg.AssignedIP)
			localIP = ip
			joinAccept = acceptMsg

			// Persist state to disk for future reboots
			os.MkdirAll(cfg.Consensus.DataDir, 0700)
			b, _ := json.Marshal(joinAccept)
			if err := os.WriteFile(statePath, b, 0600); err != nil {
				slog.Warn("Failed to save state.json", "err", err)
			}
		}
	}

	if err := ensureWireguardInterface(localIP); err != nil {
		return fmt.Errorf("failed to configure wg0 interface: %w", err)
	}

	err = wgClient.ConfigureDevice("wg0", wgtypes.Config{
		PrivateKey: &privKey,
		ListenPort: &cfg.Network.WireguardPort,
	})
	if err != nil {
		return fmt.Errorf("failed to configure wireguard device: %w", err)
	}

	if !cfg.Node.Bootstrap && joinAccept != nil {
		serverPubKey, err := wgtypes.ParseKey(joinAccept.ServerPubKey)
		if err != nil {
			return fmt.Errorf("failed to parse server public key: %w", err)
		}

		host, _, err := net.SplitHostPort(cfg.Join.Endpoint)
		if err != nil {
			return fmt.Errorf("failed to parse join endpoint: %w", err)
		}

		serverEndpoint, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", host, joinAccept.ServerWGPort))
		if err != nil {
			return fmt.Errorf("failed to resolve server udp endpoint: %w", err)
		}

		_, serverIPNet, err := net.ParseCIDR(joinAccept.ServerVPNIP + "/32")
		if err != nil {
			return fmt.Errorf("failed to parse server vpn ip: %w", err)
		}

		keepalive := 25 * time.Second
		peerCfg := wgtypes.PeerConfig{
			PublicKey:                   serverPubKey,
			Endpoint:                    serverEndpoint,
			ReplaceAllowedIPs:           true,
			AllowedIPs:                  []net.IPNet{*serverIPNet},
			PersistentKeepaliveInterval: &keepalive,
		}

		if err := wgClient.ConfigureDevice("wg0", wgtypes.Config{
			Peers: []wgtypes.PeerConfig{peerCfg},
		}); err != nil {
			return fmt.Errorf("failed to add server wg peer: %w", err)
		}

		link, err := netlink.LinkByName("wg0")
		if err == nil {
			route := &netlink.Route{
				LinkIndex: link.Attrs().Index,
				Dst:       serverIPNet,
			}
			netlink.RouteReplace(route)
		}
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

	localAPI := &LocalAPI{
		raftNode: d.raftNode,
		meshFSM:  meshFSM,
		config:   cfg,
	}

	if cfg.Node.Bootstrap {
		go func() {
			for d.raftNode.State() != raft.Leader {
				time.Sleep(1 * time.Second)
			}

			// Broadcast ourselves to the entire mesh through the Raft log
			// so followers actually receive our node information!
			cmd := network.Command{
				Opcode: network.CmdAddNode,
				Payload: fsm.Node{
					Name:           cfg.Node.Name,
					VPNIP:          localIP.String(),
					PubKey:         pubKey,
					PublicEndpoint: "",
					RaftPort:       cfg.Consensus.RaftPort,
				},
			}
			var buf bytes.Buffer
			if err := gob.NewEncoder(&buf).Encode(cmd); err == nil {
				d.raftNode.Apply(buf.Bytes(), 5*time.Second)
			}

			// Broadcast the Network CIDR to the FSM so any future leader knows how to assign IPs
			cidrCmd := network.Command{
				Opcode:  network.CmdSetNetworkCIDR,
				Payload: fmt.Sprintf("%s/%d", d.cfg.Node.NetworkLayer.IP, d.cfg.Node.NetworkLayer.CIDR),
			}
			var cidrBuf bytes.Buffer
			if err := gob.NewEncoder(&cidrBuf).Encode(cidrCmd); err == nil {
				d.raftNode.Apply(cidrBuf.Bytes(), 5*time.Second)
			}
		}()
	}

	go func() {
		errChan <- StartUnixRPC(cfg.Api.SocketPath, localAPI)
	}()

	go func() {
		errChan <- StartInternalRPC(localIP.String(), localAPI)
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

func ensureWireguardInterface(ip netip.Addr) error {
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

func joinMeshCluster(cfg config.Config, pubKey string) (*network.JoinAcceptPayload, error) {
	conn, err := net.Dial("tcp", cfg.Join.Endpoint)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	token, err := fsm.ParseToken(cfg.Security.JoinToken)
	if err != nil {
		return nil, err
	}

	wire := network.NewWire(conn)

	err = wire.Write(network.Message{
		Opcode:  network.OpHello,
		Payload: token.ID,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to send HELLO: %w", err)
	}

	wire.SetSecret(token.Secret)
	challengeMsg, err := wire.Expect(network.OpChallenge)

	if err != nil {
		return nil, err
	}

	err = wire.Write(network.Message{
		Opcode:  network.OpChallengeAccept,
		Payload: challengeMsg.Payload,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to send CHALLENGE_ACCEPT: %w", err)
	}

	err = wire.Write(network.Message{
		Opcode: network.OpJoinRequest,
		Payload: network.JoinRequestPayload{
			Name:     cfg.Node.Name,
			PubKey:   pubKey,
			WGPort:   cfg.Network.WireguardPort,
			RaftPort: cfg.Consensus.RaftPort,
		},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to send JOIN_REQUEST: %w", err)
	}

	acceptMsg, err := wire.Expect(network.OpJoinAccept)
	if err != nil {
		return nil, fmt.Errorf("failed to receive JOIN_ACCEPT: %w", err)
	}

	payload := acceptMsg.Payload.(network.JoinAcceptPayload)
	return &payload, nil
}

func (d *Daemon) getNextValidIP() (netip.Addr, error) {
	var prefixStr string
	if d.fsm.State.NetworkCIDR != "" {
		prefixStr = d.fsm.State.NetworkCIDR
	} else {
		prefixStr = fmt.Sprintf("%s/%d", d.cfg.Node.NetworkLayer.IP, d.cfg.Node.NetworkLayer.CIDR)
	}

	prefix, err := netip.ParsePrefix(prefixStr)
	if err != nil {
		return netip.Addr{}, err
	}

	candidateIP := prefix.Masked().Addr().Next()

	for {
		used := false
		for _, existingNode := range d.fsm.State.Nodes {
			if existingNode.VPNIP == candidateIP.String() {
				used = true
				break
			}
		}

		if !used {
			return candidateIP, nil
		}

		candidateIP = candidateIP.Next()

		if !prefix.Contains(candidateIP) {
			return netip.Addr{}, fmt.Errorf("IP pool exhausted")
		}
	}
}

func (d *Daemon) handleConnection(conn net.Conn) {
	defer conn.Close()

	wire := network.NewWire(conn)
	helloMsg, err := wire.Expect(network.OpHello)
	if err != nil {
		slog.Warn(fmt.Sprintf("invalid message: %s", err.Error()))
		return
	}

	tokenID := helloMsg.Payload.(string)

	token, ok := d.fsm.State.Tokens[tokenID]
	if !ok {
		return
	}

	if token.UsesLeft == 0 {
		slog.Warn("Attempted join with exhausted token", "tokenID", tokenID)
		return
	}

	wire.SetSecret(token.Secret)
	challengeData := make([]byte, 32)
	rand.Read(challengeData)

	wire.Write(network.Message{
		Opcode:  network.OpChallenge,
		Payload: challengeData,
	})

	acceptMsg, err := wire.Expect(network.OpChallengeAccept)
	if err != nil {
		slog.Warn("failed to read challenge accept", "err", err)
		return
	}

	if !bytes.Equal(acceptMsg.Payload.([]byte), challengeData) {
		slog.Warn("client failed challenge")
		return
	}

	joinMsg, err := wire.Expect(network.OpJoinRequest)
	if err != nil {
		slog.Warn(fmt.Sprintf("invalid message: %s", err.Error()))
		return
	}

	req := joinMsg.Payload.(network.JoinRequestPayload)
	clientIP := conn.RemoteAddr().(*net.TCPAddr).IP.String()

	newIP, err := d.getNextValidIP()
	if err != nil {
		slog.Error("failed to get valid IP", "err", err)
		return
	}

	newNode := fsm.Node{
		Name:           req.Name,
		VPNIP:          newIP.String(),
		PubKey:         req.PubKey,
		PublicEndpoint: fmt.Sprintf("%s:%d", clientIP, req.WGPort),
		RaftPort:       req.RaftPort,
	}

	// A) Save to Raft State
	raftCmd := network.Command{
		Opcode:  network.CmdAddNode,
		Payload: newNode,
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(raftCmd); err != nil {
		slog.Error("failed to encode raft command", "err", err)
		return
	}

	future := d.raftNode.Apply(buf.Bytes(), 2*time.Second)
	if err := future.Error(); err != nil {
		slog.Error("Failed to add node to Raft", "err", err)
		return
	}

	consumeCmd := network.Command{
		Opcode:  network.CmdConsumeToken,
		Payload: tokenID,
	}

	var consumeBuf bytes.Buffer
	if err := gob.NewEncoder(&consumeBuf).Encode(consumeCmd); err != nil {
		slog.Error("failed to encode raft command", "err", err)
		return
	}

	future = d.raftNode.Apply(consumeBuf.Bytes(), 2*time.Second)
	if err := future.Error(); err != nil {
		slog.Error("Failed to cosumeToken to Raft", "err", err)
		return
	}

	// B) Add Voter to Raft Cluster
	raftEndpoint := fmt.Sprintf("%s:%d", newNode.VPNIP, newNode.RaftPort)
	addFuture := d.raftNode.AddVoter(raft.ServerID(newNode.Name), raft.ServerAddress(raftEndpoint), 0, 0)
	if err := addFuture.Error(); err != nil {
		slog.Error("Failed to add Raft voter", "err", err)
		return
	}

	// C) Send Accept to Client
	serverNode := d.fsm.State.Nodes[d.cfg.Node.Name]

	wire.Write(network.Message{
		Opcode: network.OpJoinAccept,
		Payload: network.JoinAcceptPayload{
			AssignedIP:   newNode.VPNIP,
			ServerPubKey: serverNode.PubKey,
			ServerWGPort: d.cfg.Network.WireguardPort,
			ServerVPNIP:  serverNode.VPNIP,
		},
	})
}
