package commands

import (
	"atom/internal/api"
	"fmt"
	"log"
	"net/rpc"
)

func RevokeTokenCommand(socketPath string, tokenID string) {
	client, err := rpc.Dial("unix", socketPath)
	if err != nil {
		log.Fatalf("Daemon is not running or socket is unavailable: %v", err)
	}
	defer client.Close()

	args := &api.TokenRevokeArgs{TokenID: tokenID}
	var reply api.TokenRevokeReply

	err = client.Call("Atom.RevokeToken", args, &reply)
	if err != nil {
		log.Fatalf("RPC communication failed: %v", err)
	}

	if reply.Error != "" {
		log.Fatalf("Daemon failed to revoke token: %s", reply.Error)
	}

	fmt.Printf("Token %s revoked successfully.\n", tokenID)
}
