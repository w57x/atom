package api

// TokenCreateArgs represents the incoming binary request
type TokenCreateArgs struct {
	Uses int
}

// TokenCreateReply represents the binary response back to the CLI
type TokenCreateReply struct {
	TokenString string
	Error       string // Passed back if Raft fails
}

type TokenRevokeArgs struct {
	TokenID string
}

type TokenRevokeReply struct {
	Error string
}

type DaemonStopArgs struct{}

type DaemonStopReply struct {
	Error string
}

type NodeDestroyArgs struct {
	NodeName string
	LeaderID string
}

type NodeDestroyReply struct {
	Error string
}

type NodeRemoveArgs struct {
	NodeName string
}

type NodeRemoveReply struct {
	Error string
}

type NodeInfo struct {
	Name           string
	VPNIP          string
	PubKey         string
	PublicEndpoint string
	IsLeader       bool
	IsSelf         bool
	IsOnline       bool
}

type NodeListArgs struct{}

type NodeListReply struct {
	Nodes []NodeInfo
	Error string
}

type AuxInfo struct {
	Type        string // e.g., "phone", "pc"
	GatewayNode string // The Name of the core node acting as the gateway
	PublicKey   string // The base64 WireGuard public key of the device
}

type AuxAddArgs struct {
	Info AuxInfo
}

type AuxAddReply struct {
	Error      error
	WGConfig   string
	AssignedIP string
	DeviceId   string
}

type AuxRemoveArgs struct {
	IdPrefix string
}

type AuxRemoveReply struct {
	Error   error
	Message string
}

type AuxDeviceDTO struct {
	Id          string
	Type        string
	GatewayNode string
	PublicKey   string
	AssignedIP  string
}

type AuxListArgs struct{}

type AuxListReply struct {
	Devices []AuxDeviceDTO
	Error   string
}
