package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"time"

	"./puppetutil"
	"google.golang.org/grpc"
)

type hostFlags []string

func (i *hostFlags) String() string {
	return fmt.Sprint(*i)
}

func (i *hostFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	var hosts hostFlags
	var beginRev string
	var noop bool

	flag.Var(&hosts, "node", "node hostnames to group")
	flag.StringVar(&beginRev, "begin", "", "begin rev")
	flag.BoolVar(&noop, "noop", false, "noop")
	flag.Parse()

	// Set up a connection to the server.
	for _, host := range hosts {
		address := host + ":50051"
		log.Printf("Dialing: %v", address)
		conn, err := grpc.Dial(host+":50051", grpc.WithInsecure(), grpc.WithBlock())
		if err != nil {
			log.Fatalf("did not connect: %v", err)
		}
		defer conn.Close()
		c := puppetutil.NewRizzoClient(conn)

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*300)
		defer cancel()
		r, err := c.PuppetApply(ctx, &puppetutil.PuppetApplyRequest{Rev: beginRev, Noop: noop})
		if err != nil {
			log.Fatalf("could not apply: %v", err)
		}
		jpr, err := json.MarshalIndent(r, "", "\t")
		if err != nil {
			log.Fatalf("could not apply: %v", err)
		}
		log.Printf("%s", jpr)
	}
}
