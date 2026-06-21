package daemon

import (
	"atom/internal/api"
	"atom/internal/utils"
	"encoding/json"
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

	payload := map[string]any{
		"id":     tokenID,
		"secret": tokenSecret,
		"uses":   args.Uses,
	}

	cmdBytes, _ := json.Marshal(map[string]any{
		"opcode":  "ATOM_JOIN_TOKEN",
		"payload": payload,
	})

	// Propose to the Raft cluster
	future := api.raftNode.Apply(cmdBytes, 2*time.Second)
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
