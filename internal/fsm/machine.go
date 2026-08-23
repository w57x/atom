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
	"os"
	"strings"
	"time"

	"github.com/hashicorp/raft"
	"github.com/vishvananda/netlink"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type AuxDevice struct {
	Id          string // Unique device identifier (uuid v7)
	Type        string // e.g., "phone", "pc"
	GatewayNode string // The Name of the core node acting as the gateway
	PublicKey   string // The base64 WireGuard public key of the device
	AssignedIP  string // The assigned mesh IP (e.g., "10.0.0.45")
}

type Node struct {
	Name            string // The raft ServerID
	VPNIP           string // e.g., "10.7.0.2" (Crucial for WG AllowedIPs)
	PubKey          string // WireGuard Public Key
	PublicEndpoint  string // e.g., "82.123.45.67:51820" (The public internet IP + WG Port)
	RaftPort        uint16 // e.g., 7000
	InternalRPCPort uint16 // e.g., 7001
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
	AuxDevices  map[string]AuxDevice
}

type MeshFSM struct {
	State         *MeshState
	WGClient      *wgctrl.Client
	InterfaceName string
	StatePath     string
}

func (f *MeshFSM) PersistState() {
	if f.StatePath == "" {
		return
	}
	b, err := json.Marshal(f.State)
	if err != nil {
		slog.Error("failed to marshal mesh state", "err", err)
		return
	}
	if err := os.WriteFile(f.StatePath, b, 0600); err != nil {
		slog.Error("failed to persist mesh state to disk", "path", f.StatePath, "err", err)
	}
}

func (f *MeshFSM) LoadState() error {
	if f.StatePath == "" {
		return nil
	}
	b, err := os.ReadFile(f.StatePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := json.Unmarshal(b, &f.State); err != nil {
		return err
	}
	return nil
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

			dev, err := f.WGClient.Device(f.InterfaceName)
			if err != nil {
				slog.Error(fmt.Sprintf("Failed to get %s device", f.InterfaceName), "err", err)
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
			allowedIPs := []net.IPNet{*ipNet}

			for _, aux := range f.State.AuxDevices {
				if aux.GatewayNode == node.Name {
					_, auxIpNet, _ := net.ParseCIDR(aux.AssignedIP + "/32")
					allowedIPs = append(allowedIPs, *auxIpNet)
				}
			}

			peerCfg := wgtypes.PeerConfig{
				PublicKey:         pubKey,
				ReplaceAllowedIPs: true,
				AllowedIPs:        allowedIPs,
			}

			if len(node.PublicEndpoint) != 0 {
				endpoint, err := net.ResolveUDPAddr("udp", node.PublicEndpoint)
				if err == nil {
					peerCfg.Endpoint = endpoint
				}
			}

			err = f.WGClient.ConfigureDevice(f.InterfaceName, wgtypes.Config{
				Peers: []wgtypes.PeerConfig{peerCfg},
			})
			if err != nil {
				slog.Error("Failed to configure wg peer", "err", err)
			}

			link, err := netlink.LinkByName(f.InterfaceName)
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
				err = f.WGClient.ConfigureDevice(f.InterfaceName, wgtypes.Config{
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

	case network.CmdAddAuxDevice:
		if device, ok := cmd.Payload.(AuxDevice); ok {
			f.State.AuxDevices[device.Id] = device

			dev, err := f.WGClient.Device(f.InterfaceName)
			if err != nil {
				slog.Error(fmt.Sprintf("Failed to get %s device", f.InterfaceName), "err", err)
				return err
			}

			// Find out our own node name
			var ourNodeName string
			for name, node := range f.State.Nodes {
				if node.PubKey == dev.PublicKey.String() {
					ourNodeName = name
					break
				}
			}

			_, ipNet, _ := net.ParseCIDR(device.AssignedIP + "/32")

			if ourNodeName == device.GatewayNode {
				// CASE 1 - We are the gateway for this device
				pubKey, err := wgtypes.ParseKey(device.PublicKey)
				if err != nil {
					slog.Error("Failed to parse aux device public key", "err", err)
					return err
				}

				peerCfg := wgtypes.PeerConfig{
					PublicKey:         pubKey,
					ReplaceAllowedIPs: true,
					AllowedIPs:        []net.IPNet{*ipNet},
				}

				err = f.WGClient.ConfigureDevice(f.InterfaceName, wgtypes.Config{
					Peers: []wgtypes.PeerConfig{peerCfg},
				})
				if err != nil {
					slog.Error("Failed to configure wg peer for aux device", "err", err)
				}
			} else {
				// CASE 2 - We are not the gateway. We must update the gateway node's AllowedIPs
				gatewayNode, exists := f.State.Nodes[device.GatewayNode]
				if !exists {
					return nil // Gateway node doesn't exist yet or was removed
				}

				pubKey, err := wgtypes.ParseKey(gatewayNode.PubKey)
				if err != nil {
					slog.Error("Failed to parse gateway node public key", "err", err)
					return err
				}

				// Rebuild AllowedIPs for the gateway node
				_, gwIpNet, _ := net.ParseCIDR(gatewayNode.VPNIP + "/32")
				allowedIPs := []net.IPNet{*gwIpNet}

				for _, aux := range f.State.AuxDevices {
					if aux.GatewayNode == gatewayNode.Name {
						_, auxIpNet, _ := net.ParseCIDR(aux.AssignedIP + "/32")
						allowedIPs = append(allowedIPs, *auxIpNet)
					}
				}

				peerCfg := wgtypes.PeerConfig{
					PublicKey:         pubKey,
					ReplaceAllowedIPs: true,
					AllowedIPs:        allowedIPs,
				}

				if len(gatewayNode.PublicEndpoint) != 0 {
					endpoint, err := net.ResolveUDPAddr("udp", gatewayNode.PublicEndpoint)
					if err == nil {
						peerCfg.Endpoint = endpoint
					}
				}

				err = f.WGClient.ConfigureDevice(f.InterfaceName, wgtypes.Config{
					Peers: []wgtypes.PeerConfig{peerCfg},
				})
				if err != nil {
					slog.Error("Failed to update gateway wg peer for aux device", "err", err)
				}
			}

			// Add the route to the new IP for both cases
			link, err := netlink.LinkByName(f.InterfaceName)
			if err == nil {
				route := &netlink.Route{
					LinkIndex: link.Attrs().Index,
					Dst:       ipNet,
				}
				netlink.RouteReplace(route)
			}
		}
	case network.CmdRemoveAuxDevice:
		if deviceID, ok := cmd.Payload.(string); ok {
			if _, exists := f.State.AuxDevices[deviceID]; exists {
				slog.Info(fmt.Sprintf("Removing aux device %s from mesh", deviceID))
				delete(f.State.AuxDevices, deviceID)
				_ = f.SyncWireguard()
			}
		}
	}

	f.PersistState()
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
	f.PersistState()
	return f.SyncWireguard()
}

func (f *MeshFSM) SyncWireguard() error {
	dev, err := f.WGClient.Device(f.InterfaceName)
	if err != nil {
		return err
	}

	var ourNodeName string
	for name, node := range f.State.Nodes {
		if node.PubKey == dev.PublicKey.String() {
			ourNodeName = name
			break
		}
	}

	keepalive := 25 * time.Second
	var peers []wgtypes.PeerConfig
	for _, node := range f.State.Nodes {
		if node.PubKey == dev.PublicKey.String() {
			continue
		}

		pubKey, err := wgtypes.ParseKey(node.PubKey)
		if err != nil {
			continue
		}
		_, ipNet, err := net.ParseCIDR(node.VPNIP + "/32")
		if err != nil {
			continue
		}
		allowedIPs := []net.IPNet{*ipNet}

		for _, aux := range f.State.AuxDevices {
			if aux.GatewayNode == node.Name {
				_, auxIpNet, err := net.ParseCIDR(aux.AssignedIP + "/32")
				if err == nil {
					allowedIPs = append(allowedIPs, *auxIpNet)
				}
			}
		}

		peerCfg := wgtypes.PeerConfig{
			PublicKey:                   pubKey,
			ReplaceAllowedIPs:           true,
			AllowedIPs:                  allowedIPs,
			PersistentKeepaliveInterval: &keepalive,
		}

		if node.PublicEndpoint != "" {
			endpoint, err := net.ResolveUDPAddr("udp", node.PublicEndpoint)
			if err == nil {
				peerCfg.Endpoint = endpoint
			}
		}

		peers = append(peers, peerCfg)

		link, err := netlink.LinkByName(f.InterfaceName)
		if err == nil {
			route := &netlink.Route{
				LinkIndex: link.Attrs().Index,
				Dst:       ipNet,
			}
			netlink.RouteReplace(route)
		}
	}

	// Now we also add peers for AuxDevices where we are the GatewayNode
	for _, aux := range f.State.AuxDevices {
		_, auxIpNet, err := net.ParseCIDR(aux.AssignedIP + "/32")
		if err != nil {
			continue
		}

		if aux.GatewayNode == ourNodeName {
			pubKey, err := wgtypes.ParseKey(aux.PublicKey)
			if err == nil {
				peerCfg := wgtypes.PeerConfig{
					PublicKey:                   pubKey,
					ReplaceAllowedIPs:           true,
					AllowedIPs:                  []net.IPNet{*auxIpNet},
					PersistentKeepaliveInterval: &keepalive,
				}
				peers = append(peers, peerCfg)
			}
		}

		link, err := netlink.LinkByName(f.InterfaceName)
		if err == nil {
			route := &netlink.Route{
				LinkIndex: link.Attrs().Index,
				Dst:       auxIpNet,
			}
			netlink.RouteReplace(route)
		}
	}

	return f.WGClient.ConfigureDevice(f.InterfaceName, wgtypes.Config{
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
	gob.Register(AuxDevice{})
}
