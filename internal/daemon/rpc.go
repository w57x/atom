package daemon

import (
	"atom/config"
	"atom/internal/api"
	"atom/internal/fsm"
	"atom/internal/network"
	"atom/internal/utils"
	"bytes"
	"encoding/gob"
	"fmt"
	"log/slog"
	"net"
	"net/rpc"
	"os"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/hashicorp/raft"
)

type LocalAPI struct {
	raftNode *raft.Raft
	meshFSM  *fsm.MeshFSM
	config   config.Config
}

func (api *LocalAPI) forwardToLeader(method string, args any, reply any) error {
	leaderAddr, leaderID := api.raftNode.LeaderWithID()
	if leaderAddr == "" {
		return fmt.Errorf("no leader elected")
	}

	leaderNode, ok := api.meshFSM.GetNode(string(leaderID))
	if !ok {
		return fmt.Errorf("leader node not found in state")
	}

	internalRPCAddr := fmt.Sprintf("%s:%d", leaderNode.VPNIP, leaderNode.InternalRPCPort)

	client, err := rpc.Dial("tcp", internalRPCAddr)
	if err != nil {
		return fmt.Errorf("failed to forward request to leader: %w", err)
	}
	defer client.Close()

	return client.Call(method, args, reply)
}

func (l *LocalAPI) AddAuxiliaryDevice(args *api.AuxAddArgs, reply *api.AuxAddReply) error {
	if l.raftNode.State() != raft.Leader {
		return l.forwardToLeader("Atom.AddAuxiliaryDevice", args, reply)
	}

	if node, exists := l.meshFSM.GetNode(args.Info.GatewayNode); exists {
		newIp, err := GetNextValidIP(l.meshFSM, l.config)
		if err != nil {
			slog.Error("failed to get valid IP", "err", err)
			reply.Error = fmt.Errorf("unable to add the device :: Cause(IpAttributionFailure)")
			return nil
		}

		device := fsm.AuxDevice{
			AssignedIP:  newIp.String(),
			Id:          utils.GenerateUUID(),
			Type:        args.Info.Type,
			GatewayNode: node.Name,
			PublicKey:   args.Info.PublicKey,
		}

		cmd := network.Command{
			Opcode:  network.CmdAddAuxDevice,
			Payload: device,
		}

		var buf bytes.Buffer
		enc := gob.NewEncoder(&buf)
		if err := enc.Encode(cmd); err != nil {
			return err
		}

		applyFuture := l.raftNode.Apply(buf.Bytes(), 5*time.Second)
		if err := applyFuture.Error(); err != nil {
			reply.Error = err
		} else {
			reply.AssignedIP = device.AssignedIP
			reply.DeviceId = device.Id

			endpointLine := ""
			if node.PublicEndpoint != "" {
				endpointLine = fmt.Sprintf("Endpoint = %s\n", node.PublicEndpoint)
			}

			reply.WGConfig = fmt.Sprintf(`[Interface]
# Paste your generated PrivateKey below
PrivateKey = 
Address = %s/32
DNS = %s

[Peer]
PublicKey = %s
%sAllowedIPs = %s
PersistentKeepalive = 25`,
				device.AssignedIP,
				node.VPNIP,
				node.PubKey,
				endpointLine,
				l.meshFSM.GetNetworkCIDR())
		}
	} else {
		reply.Error = fmt.Errorf("no node named `%s` found", args.Info.GatewayNode)
	}

	return nil
}

func (l *LocalAPI) ListAuxiliaryDevices(args *api.AuxListArgs, reply *api.AuxListReply) error {
	var devices []api.AuxDeviceDTO
	for _, aux := range l.meshFSM.GetAllAuxDevices() {
		devices = append(devices, api.AuxDeviceDTO{
			Id:          aux.Id,
			Type:        aux.Type,
			GatewayNode: aux.GatewayNode,
			PublicKey:   aux.PublicKey,
			AssignedIP:  aux.AssignedIP,
		})
	}

	sort.Slice(devices, func(i, j int) bool {
		return devices[i].Id < devices[j].Id
	})

	reply.Devices = devices
	return nil
}

func (l *LocalAPI) RemoveAuxiliaryDevice(args *api.AuxRemoveArgs, reply *api.AuxRemoveReply) error {
	if l.raftNode.State() != raft.Leader {
		return l.forwardToLeader("Atom.RemoveAuxiliaryDevice", args, reply)
	}

	if args.IdPrefix == "" {
		reply.Error = fmt.Errorf("device ID prefix cannot be empty")
		return nil
	}

	match, matchCount := l.meshFSM.MatchAuxDeviceByPrefix(args.IdPrefix)

	if matchCount == 0 {
		reply.Error = fmt.Errorf("no auxiliary device found matching prefix '%s'", args.IdPrefix)
		return nil
	}
	if matchCount > 1 {
		reply.Error = fmt.Errorf("ambiguous prefix '%s' matches %d auxiliary devices", args.IdPrefix, matchCount)
		return nil
	}

	cmd := network.Command{
		Opcode:  network.CmdRemoveAuxDevice,
		Payload: match.Id,
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(cmd); err != nil {
		return err
	}

	applyFuture := l.raftNode.Apply(buf.Bytes(), 5*time.Second)
	if err := applyFuture.Error(); err != nil {
		reply.Error = err
	} else {
		reply.Message = fmt.Sprintf("Removed auxiliary device %s", match.Id)
	}

	return nil
}

func (api *LocalAPI) CreateToken(args *api.TokenCreateArgs, reply *api.TokenCreateReply) error {
	if api.raftNode.State() != raft.Leader {
		return api.forwardToLeader("Atom.CreateToken", args, reply)
	}

	payload := fsm.NewToken(args.Uses)

	cmd := network.Command{
		Opcode:  network.CmdCreateJoinToken,
		Payload: payload,
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(cmd); err != nil {
		return err
	}

	// Propose to the Raft cluster
	future := api.raftNode.Apply(buf.Bytes(), 5*time.Second)
	if err := future.Error(); err != nil {
		reply.Error = err.Error()
		return nil
	}

	reply.TokenString = payload.String()
	return nil
}

func (api *LocalAPI) RevokeToken(args *api.TokenRevokeArgs, reply *api.TokenRevokeReply) error {
	if api.raftNode.State() != raft.Leader {
		return api.forwardToLeader("Atom.RevokeToken", args, reply)
	}

	cmd := network.Command{
		Opcode:  network.CmdRevokeToken,
		Payload: args.TokenID,
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(cmd); err != nil {
		return err
	}

	future := api.raftNode.Apply(buf.Bytes(), 5*time.Second)
	if err := future.Error(); err != nil {
		reply.Error = err.Error()
	}

	return nil
}

func (api *LocalAPI) StopDaemon(args *api.DaemonStopArgs, reply *api.DaemonStopReply) error {
	slog.Info("Received API request to stop daemon. Shutting down gracefully...")
	if api.raftNode.State() == raft.Leader {
		api.raftNode.LeadershipTransfer()
	}

	// Trigger graceful shutdown by sending SIGINT to ourselves
	p, err := os.FindProcess(os.Getpid())
	if err == nil {
		p.Signal(os.Interrupt)
	}
	return nil
}

func (api *LocalAPI) DestroyNode(args *api.NodeDestroyArgs, reply *api.NodeDestroyReply) error {
	slog.Info("Received API request to destroy this node. Shutting down gracefully...")
	if api.config.Node.Name != args.NodeName {
		slog.Warn("Bogus command call (destroy)")
		return nil
	}

	// Validate authorization: must be called with the valid Raft leader ID if other nodes exist
	_, leaderID := api.raftNode.LeaderWithID()
	if args.LeaderID != "" {
		if string(leaderID) == "" || args.LeaderID != string(leaderID) {
			slog.Warn("Unauthorized destroy request: invalid or mismatched leader ID", "expected", leaderID, "got", args.LeaderID)
			reply.Error = "unauthorized: invalid leader credentials"
			return nil
		}
	} else if api.meshFSM.NodeCount() > 1 {
		slog.Warn("Unauthorized destroy request: missing leader credentials")
		reply.Error = "unauthorized: missing leader credentials"
		return nil
	}

	p, err := os.FindProcess(os.Getpid())
	if err == nil {
		os.RemoveAll(api.config.Consensus.DataDir)
		os.RemoveAll(api.config.Security.PrivateKeyPath)
		p.Signal(os.Interrupt)
	}

	return nil
}

func (l *LocalAPI) RemoveNode(args *api.NodeRemoveArgs, reply *api.NodeRemoveReply) error {
	if l.config.Node.Name == args.NodeName {
		// NOTE: If the leader is asked to remove itself, it must step down and let another node handle it.
		// Otherwise, it will destroy itself before it finishes replicating the FSM cleanup command
		if l.meshFSM.NodeCount() > 1 {
			l.raftNode.LeadershipTransfer()
			time.Sleep(1 * time.Second)
			return l.forwardToLeader("Atom.RemoveNode", args, reply)
		} else {
			// It's just us left, destroy immediately
			var stopReply api.NodeDestroyReply
			return l.DestroyNode(&api.NodeDestroyArgs{NodeName: args.NodeName}, &stopReply)
		}
	}

	if l.raftNode.State() != raft.Leader {
		return l.forwardToLeader("Atom.RemoveNode", args, reply)
	}

	// Remove from Raft Configuration
	future := l.raftNode.RemoveServer(raft.ServerID(args.NodeName), 0, 0)
	if err := future.Error(); err != nil {
		reply.Error = fmt.Sprintf("Failed to remove from raft: %v", err)
		return nil
	}

	// Fetch the node to get its VPN IP
	_, leaderID := l.raftNode.LeaderWithID()
	node, exists := l.meshFSM.GetNode(args.NodeName)
	if exists {
		// Send a DaemonStop RPC to the target node over the VPN
		// so it gracefully shuts down and doesn't get stuck in an election loop.
		rpcAddr := fmt.Sprintf("%s:%d", node.VPNIP, node.InternalRPCPort)
		client, err := rpc.Dial("tcp", rpcAddr)
		if err == nil {
			var stopReply api.NodeDestroyReply
			// We don't care if this returns an error
			_ = client.Call("Atom.DestroyNode", &api.NodeDestroyArgs{NodeName: node.Name, LeaderID: string(leaderID)}, &stopReply)
			client.Close()
		}
	}

	// Give the Raft commit and the StopDaemon RPC a brief moment to fully propagate over the network
	// before we apply the FSM command that completely destroys the WireGuard tunnel
	time.Sleep(1 * time.Second)

	// Remove from Mesh State (Cuts the Wireguard peer)
	cmd := network.Command{
		Opcode:  network.CmdRemoveNode,
		Payload: args.NodeName,
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(cmd); err != nil {
		return err
	}

	applyFuture := l.raftNode.Apply(buf.Bytes(), 5*time.Second)
	if err := applyFuture.Error(); err != nil {
		reply.Error = err.Error()
	}

	return nil
}

func (l *LocalAPI) ListNodes(args *api.NodeListArgs, reply *api.NodeListReply) error {
	_, leaderID := l.raftNode.LeaderWithID()

	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, node := range l.meshFSM.GetAllNodes() {
		wg.Add(1)
		go func(n fsm.Node) {
			defer wg.Done()

			isOnline := false
			if n.Name == l.config.Node.Name {
				isOnline = true
			} else {
				conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", n.VPNIP, n.InternalRPCPort), 1*time.Second)
				if err == nil {
					isOnline = true
					conn.Close()
				}
			}

			mu.Lock()
			reply.Nodes = append(reply.Nodes, api.NodeInfo{
				Name:           n.Name,
				VPNIP:          n.VPNIP,
				PubKey:         n.PubKey,
				PublicEndpoint: n.PublicEndpoint,
				IsLeader:       n.Name == string(leaderID),
				IsSelf:         n.Name == l.config.Node.Name,
				IsOnline:       isOnline,
			})
			mu.Unlock()
		}(node)
	}
	wg.Wait()

	sort.Slice(reply.Nodes, func(i, j int) bool {
		return reply.Nodes[i].Name < reply.Nodes[j].Name
	})

	return nil
}

func StartInternalRPC(bindIP string, localAPI *LocalAPI) error {
	addr := fmt.Sprintf("%s:%d", bindIP, localAPI.config.Api.InternalRPCPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("internal RPC failed to listen: %w", err)
	}

	server := rpc.NewServer()
	if err := server.RegisterName("Atom", localAPI); err != nil {
		return err
	}

	slog.Info("starting internal mesh API", "addr", addr)
	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}

		tcpAddr, ok := conn.RemoteAddr().(*net.TCPAddr)
		if !ok {
			conn.Close()
			continue
		}

		remoteIP := tcpAddr.IP.String()

		// Verify remote caller: aux devices are never allowed to call internal mesh RPC
		isAux := false
		for _, aux := range localAPI.meshFSM.GetAllAuxDevices() {
			if aux.AssignedIP == remoteIP {
				isAux = true
				break
			}
		}
		if isAux {
			slog.Warn("Rejecting internal RPC connection from auxiliary device", "remote_ip", remoteIP)
			conn.Close()
			continue
		}

		// Verify that caller is a registered core mesh node or self
		isKnownNode := false
		if remoteIP == bindIP {
			isKnownNode = true
		} else {
			for _, node := range localAPI.meshFSM.GetAllNodes() {
				if node.VPNIP == remoteIP {
					isKnownNode = true
					break
				}
			}
		}

		if !isKnownNode {
			slog.Warn("Rejecting internal RPC connection from unregistered IP", "remote_ip", remoteIP)
			conn.Close()
			continue
		}

		go server.ServeConn(conn)
	}
}

func StartUnixRPC(socketPath string, localAPI *LocalAPI) error {
	os.Remove(socketPath)

	oldUmask := syscall.Umask(0077)
	listener, err := net.Listen("unix", socketPath)
	syscall.Umask(oldUmask)
	if err != nil {
		return fmt.Errorf("Unable to listen to the socket %s: %w", socketPath, err)
	}

	server := rpc.NewServer()

	if err := server.RegisterName("Atom", localAPI); err != nil {
		return err
	}

	slog.Info(fmt.Sprintf("starting rpc listener @ %s", socketPath))
	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go server.ServeConn(conn)
	}
}
