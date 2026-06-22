package commands

import (
	"atom/internal/api"
	"fmt"
	"log"
	"net/rpc"
)

func StopDaemonCommand(socketPath string) {
	client, err := rpc.Dial("unix", socketPath)
	if err != nil {
		log.Fatalf("Daemon is not running or socket is unavailable: %v", err)
	}
	defer client.Close()

	args := &api.DaemonStopArgs{}
	var reply api.DaemonStopReply

	err = client.Call("Atom.StopDaemon", args, &reply)
	if err != nil {
		// It's possible the connection drops immediately if the daemon shuts down very quickly
		log.Fatalf("RPC communication failed (daemon may have stopped already): %v", err)
	}

	if reply.Error != "" {
		log.Fatalf("Daemon failed to stop: %s", reply.Error)
	}

	fmt.Println("Daemon stop signal sent successfully.")
}
