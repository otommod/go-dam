package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"

	"github.com/otommod/godam"
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

	u, err := url.Parse(playlist)
	if err != nil {
		log.Fatal(err)
	}

	fd, err := os.Create(filename)
	if err != nil {
		log.Fatal(err)
	}

	err = godam.HLS(u, fd)
	if err != nil {
		log.Fatal(err)
	}
}
