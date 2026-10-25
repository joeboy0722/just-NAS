package main

import (
	"flag"
	"just-nas/internal/vmmanager"
	"log"
)

func main() {
	mode := flag.String("mode", "web", "web, broker or guest-run")
	root := flag.String("root", "", "managed application dataset")
	socket := flag.String("socket", "", "private unix socket")
	broker := flag.String("broker", "", "private broker socket")
	guest := flag.String("guest-config", "", "validated guest launch file")
	flag.Parse()
	var e error
	switch *mode {
	case "web":
		e = vmmanager.ServeWeb(*socket, *broker)
	case "broker":
		e = vmmanager.ServeBroker(*root, *socket)
	case "guest-run":
		e = vmmanager.GuestRun(*guest)
	default:
		log.Fatal("Unsupported service mode")
	}
	if e != nil {
		log.Fatal(e)
	}
}
