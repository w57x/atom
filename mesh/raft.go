package mesh

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb"
)

// InitializeRaft sets up the consensus node.
// localID: The unique name of this node (e.g., "gateway-node-01")
// bindAddr: The internal WireGuard IP and port (e.g., "10.77.0.1:8300")
// dataDir: Where to save the BoltDB files (e.g., "/var/lib/w57x/raft")
// fsm: Your custom State Machine that configures WireGuard
// isBootstrap: True only for the very first node in the mesh
func InitializeRaft(localID string, bindAddr string, dataDir string, fsm raft.FSM, isBootstrap bool) (*raft.Raft, error) {

	// 1. Setup Raft Configuration
	config := raft.DefaultConfig()
	config.LocalID = raft.ServerID(localID)

	// Ensure the data directory exists
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create raft data dir: %w", err)
	}

	// 2. Setup Log and Stable Store (BoltDB)
	dbPath := filepath.Join(dataDir, "raft.db")
	store, err := raftboltdb.NewBoltStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create bolt store: %w", err)
	}

	// 3. Setup Snapshot Store (Retain the last 3 snapshots)
	snapshots, err := raft.NewFileSnapshotStore(dataDir, 3, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot store: %w", err)
	}

	// 4. Setup Transport Layer (TCP)
	addr, err := net.ResolveTCPAddr("tcp", bindAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve tcp addr: %w", err)
	}

	transport, err := raft.NewTCPTransport(bindAddr, addr, 3, 10*time.Second, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("failed to create tcp transport: %w", err)
	}

	// 5. Instantiate the Raft Node
	raftNode, err := raft.NewRaft(config, fsm, store, store, snapshots, transport)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize raft: %w", err)
	}

	// 6. Bootstrap the cluster (ONLY for the first node)
	if isBootstrap {
		configuration := raft.Configuration{
			Servers: []raft.Server{
				{
					ID:      config.LocalID,
					Address: transport.LocalAddr(),
				},
			},
		}
		future := raftNode.BootstrapCluster(configuration)
		if err := future.Error(); err != nil {
			return nil, fmt.Errorf("failed to bootstrap cluster: %w", err)
		}
	}

	return raftNode, nil
}

/*package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/raft"
)

// ConfigureRaft configure et démarre une instance de Raft pour un nœud donné
func ConfigureRaft(nodeID string, raftAddr string, fsm raft.FSM) (*raft.Raft, error) {

	// 1. La Configuration globale de Raft
	config := raft.DefaultConfig()
	config.LocalID = raft.ServerID(nodeID) // Identifiant unique du nœud (ex: "node-1")

	// Optionnel : Ajuster les Timers si le réseau avec votre ami est un peu lent
	config.HeartbeatTimeout = 1000 * time.Millisecond
	config.ElectionTimeout = 1000 * time.Millisecond

	// 2. Le Transport Réseau
	// C'est le canal TCP que Raft va utiliser pour faire voter les nœuds et synchroniser les logs
	address, err := net.ResolveTCPAddr("tcp", raftAddr)
	if err != nil {
		return nil, fmt.Errorf("erreur résolution adresse: %v", err)
	}

	transport, err := raft.NewTCPTransport(raftAddr, address, 3, 10*time.Second, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("erreur création transport réseau: %v", err)
	}

	// 3. Le Stockage (Logs et Stable Store)
	// Pour l'instant, on stocke TOUT en mémoire (Inmem) pour simplifier le test local.
	// (Plus tard, on utilisera un dossier sur le disque pour que ça survive aux redémarrages).
	logStore := raft.NewInmemStore()
	stableStore := raft.NewInmemStore()

	// 4. Les Snapshots (Sauvegardes instantanées)
	// Permet d'éviter que le fichier de logs ne devienne trop lourd. On utilise la mémoire aussi.
	snapshotStore := raft.NewInmemSnapshotStore()

	// 5. Création de l'instance Raft
	// On lui passe la FSM (la machine qui stockera vos clés WireGuard)
	r, err := raft.NewRaft(config, fsm, logStore, stableStore, snapshotStore, transport)
	if err != nil {
		return nil, fmt.Errorf("erreur création de l'instance Raft: %v", err)
	}

	return r, nil
}*/

/*func main() {
	nodeID := "mon-pc-local"
	raftAddr := "127.0.0.1:7000" // Port de communication Raft

	// Initialisation d'une FSM vide (à coder ensuite)
	var maFSM raft.FSM // (On verra son code juste après)

	// On configure Raft
	r, err := ConfigureRaft(nodeID, raftAddr, maFSM)
	if err != nil {
		panic(err)
	}

	// BOOTSTRAP : À faire UNIQUEMENT sur le tout premier nœud du réseau !
	// Cela dit à Raft : "Tu es tout seul pour l'instant, tu es le chef."
	configuration := raft.Configuration{
		Servers: []raft.Server{
			{
				ID:      raft.ServerID(nodeID),
				Address: raft.ServerAddress(raftAddr),
			},
		},
	}

	// On lance le cluster avec ce premier nœud
	r.BootstrapCluster(configuration)

	// Le nœud tourne en arrière plan...
	select {}
}*/

/*func main() {
	nodeID := "mon-pc-local"
	raftAddr := "127.0.0.1:7000" // Port de communication Raft

	// Initialisation d'une FSM vide (à coder ensuite)
	var maFSM raft.FSM // (On verra son code juste après)

	// On configure Raft
	r, err := ConfigureRaft(nodeID, raftAddr, maFSM)
	if err != nil {
		panic(err)
	}

	// BOOTSTRAP : À faire UNIQUEMENT sur le tout premier nœud du réseau !
	// Cela dit à Raft : "Tu es tout seul pour l'instant, tu es le chef."
	configuration := raft.Configuration{
		Servers: []raft.Server{
			{
				ID:      raft.ServerID(nodeID),
				Address: raft.ServerAddress(raftAddr),
			},
		},
	}

	// On lance le cluster avec ce premier nœud
	r.BootstrapCluster(configuration)

	// Le nœud tourne en arrière plan...
	select {}
}*/
