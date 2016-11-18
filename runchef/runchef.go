package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/context"

	"github.com/albertrdixon/gearbox/ezd"
	"github.com/albertrdixon/gearbox/logger"
	"github.com/albertrdixon/gearbox/namefmt"
	"github.com/albertrdixon/gearbox/process"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	version    = "v0.0.1"
	envFile    = "/run/metadata/chef"
	etcdEp     = []string{"http://localhost:2379", "http://localhost:22379", "http://localhost:32379"}
	etcdTo     = 5 * time.Second
	disableKey = "/chef.io/disable"
	docker     string

	app           = kingpin.New("runchef", "A wrapper for chef in environments hostile to chef.")
	logLevel      = app.Flag("log-level", "Log level.").Short('l').PlaceHolder("{debug,info,warn,error,fatal}").Default("info").Enum(logger.Levels...)
	etcdEndpoints = app.Flag("etcd-endpoint", "Etcd endpoints.").Default("localhost:2379, localhost:22379, localhost:32379").Envar("ETCD_ENDPOINT").Strings()
	pullImage     = app.Flag("pull", "Pull latest image from repo.").Default("true").Bool()
	timeout       = app.Flag("cmd-timeout", "Timeout for individual commands. Default: 5m").Default("5m").Envar("RUNCHEF_CMD_TIMEOUT").Duration()
	updateCA      = kingpin.Flag("update-ca", "Update CA bundles").Default("false").Bool()
	sslVerify     = kingpin.Flag("ssl-verify", "Use SSL verification").Default("true").Bool()
	chefDir       = client.Flag("chef-dir", "Chef directory.").Short('C').Default("/etc/chef").ExistingDir()

	disable       = app.Command("disable", "Disable chef runs for this cluster.")
	disableReason = disable.Arg("reason", "Reason for disabling.").Required().String()

	enable = app.Command("enable", "Enable chef runs for this cluster.")

	shell          = app.Command("shell", "Drop into interactive shell in chef container")
	shellImage     = shell.Flag("image", "Chef image to use").Short('i').Default("quay.io/lumoslabs/chef:latest").String()
	shellContainer = shell.Flag("container", "Chef container to use. Overrides image if set.").Short('c').String()
	shellCache     = shell.Flag("cache-name", "Chef cache container name.").Default("chef-cache").String()

	client          = app.Command("client", "Execute a chef-client run.")
	clientEnv       = client.Flag("environment", "Chef environment.").Short('e').Default("_default").Envar("CHEF_ENVIRONMENT").String()
	clientRunlist   = client.Flag("runlist", "Chef runlist.").Short('r').Default(`''`).Envar("CHEF_RUNLIST").String()
	clientName      = client.Flag("node-name", "Chef node name.").Short('n').Default(namefmt.GetName("{role}-{env}-{instanceid}.aws.lumoslabs.com")).String()
	clientImage     = client.Flag("image", "Chef image to use").Short('i').Default("quay.io/lumoslabs/chef:latest").String()
	clientContainer = client.Flag("container", "Chef container to use. Overrides image if set.").Short('c').String()
	clientCache     = client.Flag("cache-name", "Chef cache container name.").Default("chef-cache").String()
	clientForceFmt  = client.Flag("force-formatter", "Show formatter output instead of logger output.").Short('F').Default("false").Bool()
	clientLocal     = client.Flag("local", "Run in local or chef-zero mode.").Short('z').Default("false").Bool()

	clientDefault = &clientRB{
		p: "/etc/chef/client.rb",
		v: map[string]string{
			"log_location":              "STDOUT",
			"chef_server_url":           "https://chef-priv.lumoslabs.com/organizations/lumoslabs",
			"validation_client_name":    "lumoslabs-validator",
			"validation_key":            "/etc/chef/validation.pem",
			"encrypted_data_bag_secret": "/etc/chef/encrypted_data_bag_secret",
			"trusted_certs_dir":         "/etc/chef/trusted_certs",
			"cache_path":                "/chef",
			"ssl_verify_mode":           ":verify_peer",
		},
	}

	volumes = map[string]string{
		"/data":                        "/data",
		"/dev/log":                     "/dev/log",
		"/etc":                         "/etc",
		"/home":                        "/home",
		"/lib64":                       "/lib64",
		"/opt":                         "/opt",
		"/root":                        "/root",
		"/root/.kube":                  "/root/.kube",
		"/run":                         "/run",
		"/usr/bin":                     "/opt/host/bin",
		"/usr/lib64/systemd":           "/usr/lib64/systemd",
		"/usr/lib/pam.d":               "/usr/lib/pam.d",
		"/usr/lib/systemd":             "/usr/lib/systemd",
		"/usr/local/bin":               "/opt/host/local/bin",
		"/usr/local/sbin":              "/opt/host/local/sbin",
		"/usr/sbin":                    "/opt/host/sbin",
		"/usr/share":                   "/usr/share",
		"/var/run":                     "/var/run",
		"/var/run/docker.sock":         "/run/docker.sock",
		"/run/dbus/system_bus_socket":  "/run/dbus/system_bus_socket:ro",
		"/run/systemd/journal/dev-log": "/run/systemd/journal/dev-log:ro",
		"/sys/fs/cgroup":               "/sys/fs/cgroup:ro",
		"/usr/bin/docker":              "/usr/bin/docker:ro",
		"/usr/bin/etcdctl":             "/usr/bin/etcdctl:ro",
		"/usr/bin/fleetctl":            "/usr/bin/fleetctl:ro",
		"/usr/bin/gpasswd":             "/usr/bin/gpasswd:ro",
		"/usr/bin/systemctl":           "/usr/bin/systemctl:ro",
		"/usr/lib/os-release":          "/usr/lib/os-release:ro",
		"/usr/sbin/groupadd":           "/usr/sbin/groupadd:ro",
		"/usr/sbin/groupdel":           "/usr/sbin/groupdel:ro",
		"/usr/sbin/groupmod":           "/usr/sbin/groupmod:ro",
		"/usr/sbin/useradd":            "/usr/sbin/useradd:ro",
		"/usr/sbin/userdel":            "/usr/sbin/userdel:ro",
		"/usr/sbin/usermod":            "/usr/sbin/usermod:ro",
	}
)

func runChef(container, cache, env, runlist string, forceFmt, local, pull bool) error {
	cmd := append(buildClientCmd(cache),
		"--name=chef",
		container,
		"chef-client",
		fmt.Sprintf("--log_level=%s", *logLevel),
		fmt.Sprintf("--environment=%s", env),
		fmt.Sprintf("--runlist=%s", runlist),
	)
	if forceFmt {
		cmd = append(cmd, "--force-formatter")
	}
	if local {
		cmd = append(cmd, "--local-mode")
	}
	if pull {
		doPull(container)
	}
	createCache(cache)
	run("rm-chef", []string{docker, "rm", "-f", "chef"})
	return run("chef", cmd, os.Stdout)
}

func runShell(container, cache string, pull bool) error {
	cwd, er := os.Getwd()
	if er != nil {
		return er
	}

	var (
		cmd = buildClientCmd(cache)
		pa  = &os.ProcAttr{
			Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
			Dir:   cwd,
		}
	)

	cmd = append(cmd, "-ti", container, "bash")
	logger.Debugf("CMD: %v", cmd)
	if pull {
		doPull(container)
	}
	createCache(cache)
	p, er := os.StartProcess(cmd[0], cmd, pa)
	if er != nil {
		return er
	}
	_, er = p.Wait()
	return er
}

func cleanupChef() {
	run("kill-chef", []string{docker, "kill", "chef"})
	run("rm-chef", []string{docker, "rm", "-f", "chef"})
}

func run(name string, cmd []string, w ...io.Writer) error {
	var c, q = context.WithTimeout(context.Background(), *timeout)
	defer q()

	p, er := process.New(name, strings.Join(cmd, " "), w...)
	if er != nil {
		return er
	}
	logger.Debugf("cmd: %s", cmd)
	if er := p.Execute(c); er != nil {
		return er
	}
	select {
	case <-c.Done():
		return fmt.Errorf("cmd %v timed out", p)
	case <-p.Exited():
		return p.Error()
	}
}

func doPull(image string) error {
	return run("pull", []string{docker, "pull", image}, os.Stdout)
}

func createCache(name string) {
	var (
		checkCache  = []string{docker, "port", "chef-cache"}
		createCache = []string{docker, "create", fmt.Sprintf("--name=%s", name), "--volume=/chef", "busybox"}
	)

	if er := run("check-cache", checkCache); er != nil {
		run("create-cache", createCache, os.Stdout)
	}
}

func buildClientCmd(c string) []string {
	cmd := []string{docker, "run", "--rm", "--privileged", "--net=host", "--env=TZ=UTC"}
	cmd = append(cmd, fmt.Sprintf("--volumes-from=%s", c))
	for k, v := range volumes {
		cmd = append(cmd, fmt.Sprintf("--volume=%s:%s", k, v))
	}
	return cmd
}

func main() {
	app.Version(version)
	command := kingpin.MustParse(app.Parse(os.Args[1:]))
	logger.Configure(*logLevel, "[runchef] ", os.Stdout)

	switch command {
	case enable.FullCommand():
		cli, er := ezd.New(etcdEp, etcdTo)
		if er != nil {
			logger.Fatalf(er.Error())
		}

		if reason, ok := cli.Get(disableKey); ok == nil {
			if er := cli.Delete(disableKey); er != nil {
				logger.Fatalf(er.Error())
			}
			logger.Infof("Chef is now enabled! (Was disabled with reason: %s)", reason)
		} else {
			logger.Infof("Chef is already enabled.")
		}
	case disable.FullCommand():
		cli, er := ezd.New(etcdEp, etcdTo)
		if er != nil {
			logger.Fatalf(er.Error())
		}

		if reason, ok := cli.Get(disableKey); ok == nil {
			logger.Infof("Chef is already disabled with reason: %s", reason)
		} else {
			if er := cli.Set(disableKey, *disableReason); er != nil {
				logger.Fatalf(er.Error())
			}
			logger.Infof("Chef disabled with reason: %s", *disableReason)
		}
	case shell.FullCommand():
		c := *shellImage
		if len(*shellContainer) > 0 {
			c = *shellContainer
			*pullImage = false
		}
		if er := runShell(c, *shellCache, *pullImage); er != nil {
			logger.Fatalf(er.Error())
		}
	case client.FullCommand():
		c := *clientImage
		if len(*clientContainer) > 0 {
			c = *clientContainer
			*pullImage = false
		}
		cli, er := ezd.New(etcdEp, etcdTo)
		if er != nil {
			logger.Fatalf(er.Error())
		}
		if reason, ok := cli.Get(disableKey); ok == nil {
			logger.Infof("Chef is disabled: %v", reason)
			os.Exit(0)
		}
		defer cleanupChef()
		newClientRB(filepath.Join(*chefDir, "client.rb"), *clientName, *clientEnv, *sslVerify).write()
		if er := runChef(c, *clientCache, *clientEnv, *clientRunlist, *clientForceFmt, *clientLocal, *pullImage); er != nil {
			logger.Fatalf(er.Error())
		}
	}
}
