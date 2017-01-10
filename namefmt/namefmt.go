package namefmt

import (
	"io/ioutil"
	"log"
	"strings"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/mitchellh/goamz/aws"
	"github.com/mitchellh/goamz/ec2"
)

var (
	DefaultFmt = "{role}-{env}-{instanceid}"

	Timeout = 5 * time.Minute
)

func getInstanceTags(instanceid string, region string) map[string]string {
	var (
		tags = make(map[string]string)
		b    = backoff.NewExponentialBackOff()
	)
	auth, err := aws.GetAuth("", "")
	if err != nil {
		log.Fatalf("Can't get AWS auth: %v", err)
	}

	ec2conn := ec2.New(auth, aws.Regions[region])

	filter := ec2.NewFilter()
	filter.Add("resource-id", instanceid)

	fn := func() error {
		instanceTags, err := ec2conn.Tags(filter)
		if err != nil {
			return err
		}
		for _, tag := range instanceTags.Tags {
			tags[tag.Key] = tag.Value
		}
		return nil
	}

	b.MaxElapsedTime = Timeout
	backoff.Retry(fn, b)
	return tags
}

func getMeta(key string) string {
	var (
		meta []byte
		err  error
		b    = backoff.NewExponentialBackOff()
	)
	fn := func() error {
		meta, err = aws.GetMetaData(key)
		return err
	}
	b.MaxElapsedTime = Timeout
	if er := backoff.Retry(fn, b); er != nil {
		return ""
	}
	return string(meta)
}

func names() map[string]string {
	var id = ""
	if m, err := ioutil.ReadFile("/etc/machine-id"); err == nil {
		id = string(m)[0:8]
	}

	az := getMeta("placement/availability-zone")
	instance := getMeta("instance-id")
	in := ""
	if len(instance) > 0 {
		in = instance[0:10]
	}
	region := ""
	if len(az) > 0 {
		region = az[0 : len(az)-1]
	}

	tags := getInstanceTags(instance, region)

	return map[string]string{
		"machineid":  id,
		"instanceid": in,
		"region":     region,
		"az":         az,
		"role":       tags["Role"],
		"cluster":    tags["Cluster"],
		"env":        tags["Environment"],
	}
}

func expand(match map[string]string, s string) string {
	for k, v := range match {
		s = strings.Replace(s, "{"+k+"}", v, -1)
	}
	return s
}

func GetName(f string) string {
	if f == "" {
		f = DefaultFmt
	}
	return expand(names(), f)
}
