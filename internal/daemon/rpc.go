package daemon

import (
	"atom/internal/api"
	"atom/internal/fsm"
	"atom/internal/network"
	"bytes"
	"encoding/gob"
	"fmt"
	"log/slog"
	"net"
	"net/rpc"
	"os"
	"time"

	"github.com/hashicorp/raft"
)

type LocalAPI struct {
	raftNode  *raft.Raft
	meshFSM   *fsm.MeshFSM
	localName string
}

func (api *LocalAPI) forwardToLeader(method string, args any, reply any) error {
	leaderAddr, _ := api.raftNode.LeaderWithID()
	if leaderAddr == "" {
		return fmt.Errorf("no leader elected")
	}

	host, _, _ := net.SplitHostPort(string(leaderAddr))
	internalRPCAddr := fmt.Sprintf("%s:7001", host)

	client, err := rpc.Dial("tcp", internalRPCAddr)
	if err != nil {
		return fmt.Errorf("failed to forward request to leader: %w", err)
	}
	defer client.Close()

	return client.Call(method, args, reply)
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

func (api *LocalAPI) RemoveNode(args *api.NodeRemoveArgs, reply *api.NodeRemoveReply) error {
	if api.raftNode.State() != raft.Leader {
		return api.forwardToLeader("Atom.RemoveNode", args, reply)
	}

	// Remove from Raft Configuration
	future := api.raftNode.RemoveServer(raft.ServerID(args.NodeName), 0, 0)
	if err := future.Error(); err != nil {
		reply.Error = fmt.Sprintf("Failed to remove from raft: %v", err)
		return nil
	}

	// Remove from Mesh State
	cmd := network.Command{
		Opcode:  network.CmdRemoveNode,
		Payload: args.NodeName,
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(cmd); err != nil {
		return err
	}

	applyFuture := api.raftNode.Apply(buf.Bytes(), 5*time.Second)
	if err := applyFuture.Error(); err != nil {
		reply.Error = err.Error()
	}

	return nil
}

func (l *LocalAPI) ListNodes(args *api.NodeListArgs, reply *api.NodeListReply) error {
	_, leaderID := l.raftNode.LeaderWithID()

	for _, node := range l.meshFSM.State.Nodes {
		reply.Nodes = append(reply.Nodes, api.NodeInfo{
			Name:           node.Name,
			VPNIP:          node.VPNIP,
			PubKey:         node.PubKey,
			PublicEndpoint: node.PublicEndpoint,
			IsLeader:       node.Name == string(leaderID),
			IsSelf:         node.Name == l.localName,
		})
	}
	return nil
}

func StartInternalRPC(bindIP string, localAPI *LocalAPI) error {
	addr := fmt.Sprintf("%s:7001", bindIP)
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
		go server.ServeConn(conn)
	}
}

func StartUnixRPC(socketPath string, localAPI *LocalAPI) error {
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("Unable to listen to the socket %s: %w", socketPath, err)
	}

	if err := os.Chmod(socketPath, 0600); err != nil {
		return fmt.Errorf("Unable to make the socket secure: %w", err)
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
