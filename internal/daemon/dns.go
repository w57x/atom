package daemon

import (
	"atom/internal/fsm"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/miekg/dns"
)

func StartDNSServer(meshFSM *fsm.MeshFSM, bindIP string) {
	handler := func(w dns.ResponseWriter, r *dns.Msg) {
		if len(r.Question) == 0 {
			return
		}

		domain := r.Question[0].Name

		// STEP 1 :: Intercept Mesh Queries
		if strings.HasSuffix(domain, ".atom.") || strings.HasSuffix(domain, ".mesh.") {
			handleInternalDNS(w, r, domain, meshFSM)
			return
		}

		// STEP 2 :: Forward External Queries
		client := &dns.Client{Timeout: 2 * time.Second}

		var upstreams []string
		if config, err := dns.ClientConfigFromFile("/etc/resolv.conf"); err == nil && len(config.Servers) > 0 {
			port := config.Port
			if port == "" {
				port = "53"
			}
			for _, s := range config.Servers {
				upstreams = append(upstreams, s+":"+port)
			}
		}

		upstreams = append(upstreams, "8.8.8.8:53", "1.1.1.1:53")

		// we create a pristine request to avoid EDNS0/flag anomalies that cause some local routers to drop packets
		m := new(dns.Msg)
		m.SetQuestion(r.Question[0].Name, r.Question[0].Qtype)
		m.RecursionDesired = true

		var resp *dns.Msg
		var err error

		for _, upstream := range upstreams {
			resp, _, err = client.Exchange(m, upstream)
			if err == nil && resp != nil && resp.Rcode != dns.RcodeServerFailure {
				break // Success
			}
			slog.Warn("DNS upstream failed, trying next", "failed_upstream", upstream, "err", err)
		}

		if err != nil || resp == nil {
			slog.Error("All DNS upstreams failed", "err", err)
			dns.HandleFailed(w, r)
			return
		}

		// we map the ID back to the original request so the client accepts it
		resp.SetReply(r)
		w.WriteMsg(resp)
	}

	dns.HandleFunc(".", handler)
	server := &dns.Server{Addr: bindIP + ":53", Net: "udp"}

	slog.Info(fmt.Sprintf("Starting DNS forwarder on %s:53", bindIP))
	go func() {
		if err := server.ListenAndServe(); err != nil {
			slog.Error("DNS server failed", "err", err)
		}
	}()
}

func handleInternalDNS(w dns.ResponseWriter, r *dns.Msg, domain string, meshFSM *fsm.MeshFSM) {
	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.Authoritative = true

	// we strip suffix for matching (.atom. or .mesh.)
	name := strings.TrimSuffix(domain, ".atom.")
	name = strings.TrimSuffix(name, ".mesh.")

	var targetIP string

	if node, ok := meshFSM.State.Nodes[name]; ok {
		targetIP = node.VPNIP
	} else {
		for _, aux := range meshFSM.State.AuxDevices {
			if aux.Id == name {
				targetIP = aux.AssignedIP
				break
			}
		}
	}

	if targetIP != "" {
		if aRecord, err := dns.NewRR(fmt.Sprintf("%s 60 IN A %s", domain, targetIP)); err == nil {
			msg.Answer = append(msg.Answer, aRecord)
		}
	} else {
		msg.Rcode = dns.RcodeNameError // NXDOMAIN
	}

	w.WriteMsg(msg)
}
