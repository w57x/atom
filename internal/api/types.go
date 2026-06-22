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
}

type NodeListArgs struct{}

type NodeListReply struct {
	Nodes []NodeInfo
	Error string
}
