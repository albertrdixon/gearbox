package main

import (
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/albertrdixon/gearbox/logger"
)

func parseEnvFile(file string) error {
	b, er := ioutil.ReadFile(file)
	if er != nil {
		return er
	}
	for _, line := range strings.Split(string(b), "\n") {
		bits := strings.Split(line, "=")
		if len(bits) == 2 {
			logger.Debugf("Setting %s to %s", bits[0], bits[1])
			os.Setenv(bits[0], bits[1])
		}
	}
	return nil
}

func init() {
	d, er := exec.LookPath("docker")
	if er != nil {
		panic(er)
	}
	docker = d

	if _, er := os.Stat(envFile); er == nil {
		if er := parseEnvFile(envFile); er != nil {
			panic(er)
		}
	}
}
