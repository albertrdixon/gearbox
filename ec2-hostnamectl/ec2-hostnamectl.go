package main

import (
	"fmt"

	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/albertrdixon/gearbox/namefmt"
)

var (
	version = "v0.0.1"
	format  = kingpin.Flag("format", "Name format. Available tokens are machineid, instanceid, region, az, role, cluster, env").Short('f').Default(namefmt.DefaultFmt).Envar("HOSTNAMECTL_FORMAT").String()
)

func main() {
	kingpin.Version(version)
	kingpin.Parse()
	fmt.Print(namefmt.GetName(*format))
}
