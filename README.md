# <img src="assets/atom-logo.svg" width="100" alt="Atom Logo" align="left"> Atom

<div align="right">

**Atom** is a fully decentralized, self-healing, zero-trust mesh VPN. It marries
the cryptographically secure, high-performance networking of **WireGuard** with
the distributed consensus of **HashiCorp Raft**.

</div>

Unlike traditional hub-and-spoke VPNs or centrally coordinated overlays, Atom
has **no single point of failure** and **no central coordination server**.
The mesh completely orchestrates itself.

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

- **Embedded DNS Proxy:** A built-in split-tunnel DNS server securely resolves
  internal `.atom` addresses (like `node-1.atom`) while proxying standard
  internet traffic to your host's native resolvers with fallbacks.

## Architecture

Atom abstracts away the complexity of managing WireGuard keys and OS-level routing.

1. **The Daemon:** Runs as a background service. It manages the WireGuard `wg0`
   interface, enforces strict `/32` CIDR OS routing, and runs the Raft state machine.
2. **The FSM (Finite State Machine):** Replicates the mesh state. When a
   new node joins, the FSM adds the WireGuard peer to every machine. When a node
   is removed, the FSM instantly cuts the cryptographic tunnel on all nodes.
3. **The CLI:** A lightweight client that talks to the local daemon via a Unix socket (`/tmp/atom.sock`).

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

security:
  private_key_path: "/etc/atom/private.key"
  join_token: "atom.<id>.<secret>"

network:
  wireguard_port: 51820

consensus:
  raft_port: 7000
  data_dir: "/var/lib/atom/data"

api:
  socket_path: "/tmp/atom.sock"
```

## Usage

### 1. Start the Daemon

Start the background daemon. If this is the bootstrapper, it will initialize the cluster. If it's a follower, it will perform the cryptographic handshake and join.

```bash
# Start the daemon in the background with the join token flag
atom daemon -c config.yaml --join-token <TOKEN_HERE> &
```

_You can also pass the token using the `ATOM_JOIN_TOKEN` environment variable_

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

### 6. Auxiliary Devices

You can attach unmanaged "Auxiliary Devices" (like phones or laptops) directly
to the mesh without running the Atom daemon on them. The mesh handles routing
via a designated Gateway node and provides split-tunnel DNS resolution natively.

To add an auxiliary device:

```bash
atom aux add --type phone --from node-2 --pubkey <base64_pubkey>
```

_This will generate a standard WireGuard `.conf`_

Manage auxiliary devices:

```bash
atom aux list
atom aux remove <id_prefix>
```

#### Troubleshooting Routing and Auxiliary Devices

When using auxiliary devices, the designated Gateway node physically routes
traffic between the auxiliary device and the rest of the mesh.

**1. Kernel IP Forwarding**
For the Linux kernel to allow routing traffic between peers, IP forwarding must
be enabled on the Gateway node:

```bash
sudo sysctl -w net.ipv4.ip_forward=1
```

_(To make this permanent, add `net.ipv4.ip_forward = 1` to `/etc/sysctl.conf`)_

**2. UFW Forwarding Policy**
If you are using `ufw` on the Gateway node, its default `FORWARD` policy is
`DROP`, which will silently discard TCP traffic (though ICMP pings might still
work due to global before-rules).

To fix TCP timeouts, explicitly allow UFW to route traffic between WireGuard
interfaces on the Gateway node:

```bash
sudo ufw route allow in on wg0 out on wg0
```

### 7. Internal DNS System

Atom includes a native, embedded DNS server that binds to the WireGuard
interface (port 53). It automatically resolves internal node names.

- **Internal Resolution:** Querying `node-1.atom` will perfectly resolve to its
  assigned VPN IP (e.g., `10.7.0.1`).
- **Split-Tunnel Proxying:** Any non-`.atom` queries are seamlessly proxied to
  the host OS's native DNS servers (found in `/etc/resolv.conf`). If the local
  network DNS fails or times out, it features a fallback to `8.8.8.8` and `1.1.1.1`.

If you use an Auxiliary Device like a mobile phone, just set the DNS server in
your WireGuard app to the Gateway node's VPN IP, and everything will perfectly
resolve.
