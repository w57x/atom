package fsm

import (
	"atom/internal/network"
	"atom/internal/utils"
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"

	"github.com/hashicorp/raft"
	"github.com/vishvananda/netlink"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type Node struct {
	Name           string // The raft ServerID
	VPNIP          string // e.g., "10.7.0.2" (Crucial for WG AllowedIPs)
	PubKey         string // WireGuard Public Key
	PublicEndpoint string // e.g., "82.123.45.67:51820" (The public internet IP + WG Port)
	RaftPort       int    // e.g., 7000
}

type Token struct {
	ID       string
	Secret   string
	UsesLeft int
}

func NewToken(usesLeft int) Token {
	return Token{
		ID:       utils.GenerateID(),
		Secret:   utils.GenerateSecret(),
		UsesLeft: usesLeft,
	}
}

func ParseToken(token string) (Token, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Token{}, fmt.Errorf("Invalid token format")
	}

	if parts[0] != "atom" {
		return Token{}, fmt.Errorf("Invalid token format")
	}

	return Token{ID: parts[1], Secret: parts[2], UsesLeft: -1}, nil
}

func (t *Token) String() string {
	return fmt.Sprintf("atom.%s.%s", t.ID, t.Secret)
}

type MeshState struct {
	NetworkCIDR string
	Nodes       map[string]Node
	Tokens      map[string]Token
}

type MeshFSM struct {
	State    *MeshState
	WGClient *wgctrl.Client
}

func (f *MeshFSM) Apply(log *raft.Log) any {
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
	case network.CmdAddNode:
		if node, ok := cmd.Payload.(Node); ok {
			slog.Info(fmt.Sprintf("New joined node %s", node.Name))
			f.State.Nodes[node.Name] = node

			dev, err := f.WGClient.Device("wg0")
			if err != nil {
				slog.Error("Failed to get wg0 device", "err", err)
				return err
			}

			// Do not add ourselves as a peer
			if node.PubKey == dev.PublicKey.String() {
				return nil
			}

			pubKey, err := wgtypes.ParseKey(node.PubKey)
			if err != nil {
				slog.Error("Failed to parse node public key", "err", err)
				return err
			}

			_, ipNet, _ := net.ParseCIDR(node.VPNIP + "/32")

			peerCfg := wgtypes.PeerConfig{
				PublicKey:         pubKey,
				ReplaceAllowedIPs: true,
				AllowedIPs:        []net.IPNet{*ipNet},
			}

			if len(node.PublicEndpoint) != 0 {
				endpoint, err := net.ResolveUDPAddr("udp", node.PublicEndpoint)
				if err == nil {
					peerCfg.Endpoint = endpoint
				}
			}

			err = f.WGClient.ConfigureDevice("wg0", wgtypes.Config{
				Peers: []wgtypes.PeerConfig{peerCfg},
			})
			if err != nil {
				slog.Error("Failed to configure wg peer", "err", err)
			}

			link, err := netlink.LinkByName("wg0")
			if err == nil {
				route := &netlink.Route{
					LinkIndex: link.Attrs().Index,
					Dst:       ipNet,
				}
				netlink.RouteReplace(route)
			}
		}
	case network.CmdConsumeToken:
		if tokenID, ok := cmd.Payload.(string); ok {
			if token, exists := f.State.Tokens[tokenID]; exists {
				token.UsesLeft--
				if token.UsesLeft <= 0 {
					slog.Info(fmt.Sprintf("Consumed all uses for token %s", tokenID))
					delete(f.State.Tokens, tokenID)
				} else {
					f.State.Tokens[tokenID] = token
				}
			}
		}
	case network.CmdRevokeToken:
		if tokenID, ok := cmd.Payload.(string); ok {
			slog.Info(fmt.Sprintf("Revoking token %s", tokenID))
			delete(f.State.Tokens, tokenID)
		}
	case network.CmdRemoveNode:
		if nodeName, ok := cmd.Payload.(string); ok {
			node, exists := f.State.Nodes[nodeName]
			if !exists {
				return nil
			}

			slog.Info(fmt.Sprintf("Removing node %s from mesh", node.Name))
			delete(f.State.Nodes, nodeName)

			// Remove from WireGuard peer list
			pubKey, err := wgtypes.ParseKey(node.PubKey)
			if err == nil {
				err = f.WGClient.ConfigureDevice("wg0", wgtypes.Config{
					Peers: []wgtypes.PeerConfig{
						{
							PublicKey: pubKey,
							Remove:    true,
						},
					},
				})
				if err != nil {
					slog.Error("Failed to remove wg peer", "err", err)
				}
			}
		}

	case network.CmdSetNetworkCIDR:
		if cidr, ok := cmd.Payload.(string); ok {
			f.State.NetworkCIDR = cidr
			slog.Info("Network CIDR updated", "cidr", cidr)
		} else {
			slog.Error("Invalid payload type for CmdSetNetworkCIDR")
		}
	}

	return nil
}

func (f *MeshFSM) Snapshot() (raft.FSMSnapshot, error) {
	b, err := json.Marshal(f.State)
	if err != nil {
		return nil, err
	}
	return &fsmSnapshot{stateBytes: b}, nil
}

func (f *MeshFSM) Restore(rc io.ReadCloser) error {
	if err := json.NewDecoder(rc).Decode(&f.State); err != nil {
		return err
	}

	dev, err := f.WGClient.Device("wg0")
	if err != nil {
		return err
	}

	var peers []wgtypes.PeerConfig
	for _, node := range f.State.Nodes {
		if node.PubKey == dev.PublicKey.String() {
			continue
		}

		pubKey, _ := wgtypes.ParseKey(node.PubKey)
		_, ipNet, _ := net.ParseCIDR(node.VPNIP + "/32")

		peerCfg := wgtypes.PeerConfig{
			PublicKey:         pubKey,
			ReplaceAllowedIPs: true,
			AllowedIPs:        []net.IPNet{*ipNet},
		}

		if node.PublicEndpoint != "" {
			endpoint, _ := net.ResolveUDPAddr("udp", node.PublicEndpoint)
			peerCfg.Endpoint = endpoint
		}

		peers = append(peers, peerCfg)

		link, err := netlink.LinkByName("wg0")
		if err == nil {
			route := &netlink.Route{
				LinkIndex: link.Attrs().Index,
				Dst:       ipNet,
			}
			netlink.RouteReplace(route)
		}
	}

	return f.WGClient.ConfigureDevice("wg0", wgtypes.Config{
		ReplacePeers: true,
		Peers:        peers,
	})
}

type fsmSnapshot struct {
	stateBytes []byte
}

// Persist writes the state to the Raft disk sink
func (s *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	_, err := sink.Write(s.stateBytes)
	if err != nil {
		sink.Cancel() // Abort if there's an error writing
		return err
	}
	return sink.Close()
}

func (s *fsmSnapshot) Release() {}

func init() {
	gob.Register(Token{})
	gob.Register(Node{})
}
