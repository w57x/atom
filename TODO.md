# Atom - Future Polish and Enhancements

## System Integration

- [ ] **Systemd Service Files**: Create a `.service` file configuration so the
      Atom daemon can run continuously in the background on boot, properly
      handle crashes, and manage logs through journalctl.

## Networking Features

- [ ] **Internal DNS (Mesh Naming)**: Integrate a lightweight DNS server
      (e.g. CoreDNS) directly into the daemon. This will allow nodes to ping
      and resolve each other by their configured hostnames (e.g. `ping node-2`)
      instead of requiring the user to memorize the `10.7.0.x` VPN IPs.
- [ ] **Advanced NAT Traversal (STUN/TURN)**: WireGuard's native NAT punching
      works for most home routers, but fails on strict enterprise symmetric
      firewalls. Implement a fallback mechanism using STUN/TURN relays
      (similar to Tailscale's DERP servers) to guarantee 100% connectivity
      between nodes in all network conditions.
