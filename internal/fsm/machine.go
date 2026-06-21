package fsm

import (
	"encoding/json"
	"io"

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
	// Execute binary decoding and state mutation here
	return nil
}

func (f *MeshFSM) Snapshot() (raft.FSMSnapshot, error) {
	return nil, nil
}

func (f *MeshFSM) Restore(rc io.ReadCloser) error {
	return json.NewDecoder(rc).Decode(&f.State)
}
