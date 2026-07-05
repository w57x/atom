package commands

import (
	"atom/internal/api"
	"encoding/json"
	"fmt"
	"log"
	"net/rpc"
	"os"

	"github.com/logrusorgru/aurora/v4"
	"github.com/olekukonko/tablewriter"
)

func ListAuxiliaryDevices(socketPath string, asJson bool) {
	client, err := rpc.Dial("unix", socketPath)
	if err != nil {
		log.Fatalf("Daemon is not running or socket is unavailable: %v", err)
	}
	defer client.Close()

	var reply api.AuxListReply
	err = client.Call("Atom.ListAuxiliaryDevices", &api.AuxListArgs{}, &reply)
	if err != nil {
		log.Fatalf("RPC communication failed %v", err)
	}

	if reply.Error != "" {
		log.Fatalf("An error occured: %s", reply.Error)
	}

	if asJson {
		data, err := json.MarshalIndent(reply.Devices, "", "  ")
		if err != nil {
			panic(err)
		}
		fmt.Printf("%s\n", string(data))
		return
	}

	if len(reply.Devices) == 0 {
		fmt.Println("No auxiliary devices found.")
		return
	}

	var data [][]any
	for _, device := range reply.Devices {
		pubKeyStr := device.PublicKey
		if len(pubKeyStr) > 16 {
			pubKeyStr = pubKeyStr[:16] + "..."
		}

		idStr := device.Id
		if len(idStr) > 8 {
			idStr = idStr[:8]
		}

		data = append(data, []any{
			fmt.Sprint(aurora.Magenta(idStr)),
			fmt.Sprint(aurora.Blue(device.Type)),
			fmt.Sprint(aurora.Cyan(device.AssignedIP)),
			fmt.Sprint(aurora.Yellow(device.GatewayNode)),
			fmt.Sprint(aurora.Gray(10, pubKeyStr)),
		})
	}

	table := tablewriter.NewTable(os.Stdout)
	table.Header("ID", "TYPE", "VPN IP", "GATEWAY", "PUBKEY")
	table.Bulk(data)
	if err := table.Render(); err != nil {
		log.Fatalf("Failed to render table: %v", err)
	}
	fmt.Println()
}
