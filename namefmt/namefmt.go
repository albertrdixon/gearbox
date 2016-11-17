package namefmt

import (
	"io/ioutil"
	"log"
	"strings"

	"github.com/mitchellh/goamz/aws"
	"github.com/mitchellh/goamz/ec2"
)

var DefaultFmt = "{role}-{env}-{instanceid}"

func getInstanceTags(instanceid string, region string) map[string]string {
	auth, err := aws.GetAuth("", "")
	if err != nil {
		log.Fatalf("Can't get AWS auth: %v", err)
	}

	ec2conn := ec2.New(auth, aws.Regions[region])

	filter := ec2.NewFilter()
	filter.Add("resource-id", instanceid)

	instanceTags, err := ec2conn.Tags(filter)
	if err != nil {
		log.Fatalf("Cannot query instance tags: %v", err)
	}

	tags := make(map[string]string)
	for _, tag := range instanceTags.Tags {
		tags[tag.Key] = tag.Value
	}

	return tags
}

func getMeta(key string) string {
	meta, err := aws.GetMetaData(key)
	if err != nil {
		log.Fatalf("AWS metadata key cannot be determined: %v", err)
	}
	return string(meta)
}

func names() map[string]string {

	machineid, err := ioutil.ReadFile("/etc/machine-id")
	if err != nil {
		log.Fatal("machine-id cannot be determined.")
	}

	instance := getMeta("instance-id")
	az := getMeta("placement/availability-zone")
	region := az[0 : len(az)-1]

	tags := getInstanceTags(instance, region)

	return map[string]string{
		"machineid":  string(machineid)[0:8],
		"instanceid": instance[0:10],
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
