package commands

import (
	"atom/internal/api"
	"encoding/json"
	"fmt"
	"log"
	"net/rpc"
)

func AddAuxiliaryDevice(socketPath string, info api.AuxInfo, asJson bool) {
	client, err := rpc.Dial("unix", socketPath)
	if err != nil {
		log.Fatalf("Daemon is not running or socket is unavailable: %v", err)
	}
	defer client.Close()

	args := &api.AuxAddArgs{Info: info}
	var reply api.AuxAddReply

	err = client.Call("Atom.AddAuxiliaryDevice", args, reply)
	if err != nil {
		log.Fatalf("RPC communication failed %v", err)
	}

	if reply.Error != nil {
		log.Fatalf("An error occured: %s", reply.Error.Error())
	}

	if asJson {
		data, err := json.Marshal(reply)
		if err != nil {
			panic(err)
		}

		fmt.Printf("%s", string(data))
		return
	}

	fmt.Printf("%s\n\n", reply.WGConfig)
	fmt.Printf("IP        :: %s\n", reply.AssignedIP)
	fmt.Printf("Device ID :: %s\n", reply.DeviceId)
}
