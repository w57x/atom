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
