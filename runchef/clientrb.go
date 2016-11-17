package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/albertrdixon/gearbox/logger"
)

type clientRB struct {
	p string
	v map[string]string
}

func (c *clientRB) merge(d *clientRB) *clientRB {
	for k, v := range d.v {
		if _, ok := c.v[k]; !ok {
			c.v[k] = v
		}
	}
	return c
}

func (c *clientRB) write() error {
	f, er := os.Create(c.p)
	if er != nil {
		return er
	}
	w := bufio.NewWriter(f)
	defer f.Close()
	for k, v := range c.v {
		format := `%s "%s"`
		if k == "log_location" || k == "ssl_verify_mode" {
			format = `%s %s`
		}
		fmt.Fprintln(w, format, k, v)
	}
	return w.Flush()
}

func newClientRB(cPath, name, environment string, sslVerify bool) *clientRB {
	var (
		c = &clientRB{p: cPath, v: make(map[string]string, 0)}
	)
	if len(c.p) < 1 {
		c.p = clientDefault.p
	}
	for k, v := range clientDefault.v {
		c.v[k] = v
	}
	c.v["node_name"] = name
	c.v["environment"] = environment
	if !sslVerify {
		c.v["ssl_verify_peer"] = ":verify_none"
	}
	_, ok := os.Stat(c.p)
	if ok == nil {
		c = readClientRB(c.p).merge(c)
	}
	return c
}

func readClientRB(p string) *clientRB {
	var c = &clientRB{p: p, v: make(map[string]string, 0)}
	f, er := ioutil.ReadFile(p)
	if er != nil {
		return nil
	}
	for _, line := range strings.Split(string(f), "\n") {
		bits := strings.Fields(line)
		if len(bits) == 2 {
			logger.Debugf("client.rb: %s = %s", bits[0], bits[1])
			c.v[bits[0]] = bits[1]
		}
	}
	return c
}
