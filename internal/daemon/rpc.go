package daemon

import (
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
	"time"

	"github.com/hashicorp/raft"
)

type LocalAPI struct {
	raftNode *raft.Raft
}

func (api *LocalAPI) CreateToken(args *api.TokenCreateArgs, reply *api.TokenCreateReply) error {
	tokenID := utils.GenerateID()
	tokenSecret := utils.GenerateSecret()

	payload := fsm.Token{
		ID:       tokenID,
		Secret:   tokenSecret,
		UsesLeft: args.Uses,
	}

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

	reply.TokenString = fmt.Sprintf("atom_%s_%s", tokenID, tokenSecret)
	return nil
}

func StartUnixRPC(socketPath string, r *raft.Raft) error {
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("Unable to listen to the socket %s: %w", socketPath, err)
	}

	if err := os.Chmod(socketPath, 0600); err != nil {
		return fmt.Errorf("Unable to make the socket secure: %w", err)
	}

	server := rpc.NewServer()
	localAPI := &LocalAPI{raftNode: r}

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
