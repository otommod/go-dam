package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/grafov/m3u8"
	"github.com/otommod/go-dam/hls"
)

var (
	format = flag.String("format", "best", "Which quality to download")
	debug  = flag.Bool("debug", false, "Enable debugging messages")
)

func printUsageLine() {
	fmt.Fprintf(flag.CommandLine.Output(),
		"Usage: %s [options] playlist-url output-file\n", flag.CommandLine.Name())
}

func main() {
	flag.Usage = func() {
		printUsageLine()
		flag.PrintDefaults()
	}

	flag.Parse()

	if len(flag.CommandLine.Args()) < 3 {
		printUsageLine()
	}

	playlist := flag.CommandLine.Arg(0)
	filename := flag.CommandLine.Arg(1)

	fd, err := os.Create(filename)
	if err != nil {
		log.Fatal(err)
	}

	hlsClient := hls.Client{
		Client: http.DefaultClient,
	}

	variants, err := hlsClient.ListVariants(playlist)
	if err != nil {
		log.Fatal(err)
	}

	var highestBandwidth *m3u8.Variant
	for _, v := range variants {
		if highestBandwidth == nil || v.Bandwidth > highestBandwidth.Bandwidth {
			highestBandwidth = v
		}
	}

	err = hlsClient.Download(context.TODO(), highestBandwidth.URI, fd)
	if err != nil {
		log.Fatal(err)
	}
}
