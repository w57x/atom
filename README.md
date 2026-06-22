# <img src="assets/atom-logo.svg" width="50" alt="Atom Logo" align="top"> Atom

**Atom** is a fully decentralized, self-healing, zero-trust mesh VPN. It marries
the cryptographically secure, high-performance networking of **WireGuard** with
the distributed consensus of **HashiCorp Raft**.

Unlike traditional hub-and-spoke VPNs or centrally coordinated overlays, Atom
has **no single point of failure** and **no central coordination server**.
The mesh completely orchestrates itself.

---

## Features

- **True Decentralization:** Every node runs the Raft consensus protocol. If
  the Bootstrapper node dies, the remaining nodes instantly elect a new Leader
  and the mesh continues to function flawlessly.

- **Self-Healing State:** The Mesh Finite State Machine (FSM) is the single
  source of truth. Changes to the network (adding/removing nodes) are replicated
  across the Raft log and deterministically applied to the OS networking layer.

- **Internal Mesh RPC:** Atom binds a secure management RPC listener exclusively
  to the internal WireGuard interface (`wg0`). CLI commands issued on
  _any_ Follower node are securely tunneled over the VPN to the Leader, making
  the global mesh feel like a single local machine.

- **Amnesic Eviction:** Removing a node entirely wipes its cryptographic keys
  and Raft database. It cleanly exits and cannot resurrect itself as a "zombie" node.

- **Graceful Handshaking:** Nodes join via a one-time cryptographic token.
  The Bootstrapper securely assigns IPs and propagates the new WireGuard
  public keys across the entire mesh in milliseconds.

---

## Architecture

Atom abstracts away the complexity of managing WireGuard keys and OS-level routing.

1. **The Daemon:** Runs as a background service. It manages the WireGuard `wg0`
   interface, enforces strict `/32` CIDR OS routing, and runs the Raft state machine.
2. **The FSM (Finite State Machine):** Replicates the mesh state. When a
   new node joins, the FSM adds the WireGuard peer to every machine. When a node
   is removed, the FSM instantly cuts the cryptographic tunnel on all nodes.
3. **The CLI:** A lightweight client that talks to the local daemon via a Unix socket (`/tmp/atom.sock`).

---

## Getting Started

### Building from Source

```bash
git clone https://github.com/w57x/atom.git
cd atom
just build
```

_You can check out the release page for pre-compiled binaries_

### Configuration

Generate a heavily-documented default configuration file using the built-in generator:

```bash
atom confgen --output config.yaml
```

This will drop a file with all fields explained. You will need to customize this
depending on whether the node is a bootstrapper or a follower.

**Bootstrapper (Node 1):**

```yaml
node:
  name: "node-1"
  bootstrap: true
  network_layer:
    ip: "10.7.0.0"
    cidr: 24

security:
  private_key_path: "/etc/atom/private.key"

network:
  wireguard_port: 51820
  tcp_join_port: 8080

consensus:
  raft_port: 7000
  data_dir: "/var/lib/atom/data"

api:
  socket_path: "/tmp/atom.sock"
```

**Follower (Node 2):**
Followers do not define the `network_layer` (the Leader dynamically assigns it from the replicated FSM).

```yaml
node:
  name: "node-2"
  bootstrap: false

join:
  endpoint: "192.168.1.100:8080"
  token: "atom.<id>.<secret>"

security:
  private_key_path: "/etc/atom/private.key"

network:
  wireguard_port: 51820

consensus:
  raft_port: 7000
  data_dir: "/var/lib/atom/data"

api:
  socket_path: "/tmp/atom.sock"
```

---

## Usage

### 1. Start the Daemon

Start the background daemon. If this is the bootstrapper, it will initialize the cluster. If it's a follower, it will perform the cryptographic handshake and join.

```bash
atom daemon start -c config.yaml
```

### 2. Create Join Tokens

On any node (requests are automatically forwarded to the Leader), generate a single-use token for a new node to join.

```bash
atom token create --uses 1
```

### 3. List Nodes

View a table of all nodes currently participating in the mesh consensus.

```bash
atom node list
```

_(Tip: Add `-j` or `--json` to output raw JSON for scripting)._

### 4. Remove a Node

Gracefully assassinate a node. The Leader will shrink the Raft quorum, securely
notify the target node to wipe its identity and shut down, and drop its
WireGuard cryptographic tunnel mesh-wide.

```bash
atom node remove node-2
```

### 5. Stop the Daemon

Gracefully stop the local daemon. If the daemon is the current Raft Leader, it
will securely transfer leadership to a Follower before shutting down.

```bash
atom daemon stop
```
