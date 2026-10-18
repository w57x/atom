package fsm

import (
	"sync"
	"testing"
)

func TestMeshFSMConcurrentAccess(t *testing.T) {
	meshFSM := &MeshFSM{
		State: &MeshState{
			Nodes:      make(map[string]Node),
			Tokens:     make(map[string]Token),
			AuxDevices: make(map[string]AuxDevice),
		},
	}

	var wg sync.WaitGroup
	// 50 concurrent readers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = meshFSM.GetAllNodes()
				_ = meshFSM.GetAllAuxDevices()
				_, _ = meshFSM.LookupInternalDNS("node-1")
				_, _ = meshFSM.GetToken("tok-1")
			}
		}()
	}

	// 10 concurrent writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				meshFSM.Mu.Lock()
				meshFSM.State.Nodes["node-1"] = Node{Name: "node-1", VPNIP: "10.7.0.1"}
				meshFSM.State.Tokens["tok-1"] = Token{ID: "tok-1", UsesLeft: 1}
				meshFSM.State.AuxDevices["aux-1"] = AuxDevice{Id: "aux-1", AssignedIP: "10.7.0.2"}
				meshFSM.Mu.Unlock()
			}
		}(i)
	}

	wg.Wait()
}

func TestMatchAuxDeviceByPrefix(t *testing.T) {
	meshFSM := &MeshFSM{
		State: &MeshState{
			Nodes:  make(map[string]Node),
			Tokens: make(map[string]Token),
			AuxDevices: map[string]AuxDevice{
				"device-abc-123": {Id: "device-abc-123", AssignedIP: "10.7.0.5"},
				"device-xyz-789": {Id: "device-xyz-789", AssignedIP: "10.7.0.6"},
			},
		},
	}

	// Empty prefix must match 0 devices (preventing accidental deletion of all/first)
	_, count := meshFSM.MatchAuxDeviceByPrefix("")
	if count != 0 {
		t.Errorf("expected empty prefix to match 0 devices, got %d", count)
	}

	// Unique prefix
	match, count := meshFSM.MatchAuxDeviceByPrefix("device-abc")
	if count != 1 || match.Id != "device-abc-123" {
		t.Errorf("expected unique match for device-abc-123, got count=%d, id=%s", count, match.Id)
	}

	// Ambiguous prefix
	_, count = meshFSM.MatchAuxDeviceByPrefix("device-")
	if count != 2 {
		t.Errorf("expected ambiguous match count=2, got %d", count)
	}

	// Non-existent prefix
	_, count = meshFSM.MatchAuxDeviceByPrefix("nonexistent")
	if count != 0 {
		t.Errorf("expected count=0, got %d", count)
	}
}

func TestLookupInternalDNS(t *testing.T) {
	meshFSM := &MeshFSM{
		State: &MeshState{
			Nodes: map[string]Node{
				"gateway-1": {Name: "gateway-1", VPNIP: "10.7.0.1"},
			},
			Tokens: make(map[string]Token),
			AuxDevices: map[string]AuxDevice{
				"phone-1": {Id: "phone-1", AssignedIP: "10.7.0.10"},
			},
		},
	}

	ip, found := meshFSM.LookupInternalDNS("gateway-1")
	if !found || ip != "10.7.0.1" {
		t.Errorf("expected 10.7.0.1, got %s (found: %v)", ip, found)
	}

	ip, found = meshFSM.LookupInternalDNS("phone-1")
	if !found || ip != "10.7.0.10" {
		t.Errorf("expected 10.7.0.10, got %s (found: %v)", ip, found)
	}

	_, found = meshFSM.LookupInternalDNS("unknown")
	if found {
		t.Errorf("expected unknown domain not to be found")
	}
}
