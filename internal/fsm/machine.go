package fsm

import (
	"atom/internal/network"
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/hashicorp/raft"
	"golang.zx2c4.com/wireguard/wgctrl"
)

type Node struct {
	PubKey   string
	Endpoint string
	RaftPort int
}

type Token struct {
	ID       string
	Secret   string
	UsesLeft int
}

type MeshState struct {
	Nodes  map[string]Node
	Tokens map[string]Token
}

type MeshFSM struct {
	State    *MeshState
	WGClient *wgctrl.Client
}

func (f *MeshFSM) Apply(log *raft.Log) any {
	// NOTE: Execute binary decoding and state mutation here

	var cmd network.Command

	buf := bytes.NewReader(log.Data)
	dec := gob.NewDecoder(buf)

	if err := dec.Decode(&cmd); err != nil {
		slog.Error(fmt.Sprintf("failed to decode raft log: %v", err))
		return err
	}

	switch cmd.Opcode {
	case network.CmdCreateJoinToken:
		if token, ok := cmd.Payload.(Token); ok {
			slog.Info(fmt.Sprintf("New Join token with id %s", token.ID))
			f.State.Tokens[token.ID] = token
		}
	}

	return nil
}

func (f *MeshFSM) Snapshot() (raft.FSMSnapshot, error) {
	return nil, nil
}

func (f *MeshFSM) Restore(rc io.ReadCloser) error {
	return json.NewDecoder(rc).Decode(&f.State)
}

func init() {
	gob.Register(Token{})
	gob.Register(Node{})
}
