package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"syscall"

	flag "github.com/ogier/pflag"
	"github.com/rancher/go-rancher-metadata/metadata"
)

var (
	metadataUrl *string = flag.String("metadata", "http://rancher-metadata/2015-12-19", "Metadata URL")
)

func main() {
	flag.Parse()
	log.Fatal(run())
}

func run() error {
	m, err := metadata.NewClientAndWait(*metadataUrl)
	if err != nil {
		return err
	}

	host, err := m.GetSelfHost()
	if err != nil {
		return err
	}

	hostsconf, err := ioutil.TempFile("", "dnsmasq-hosts")
	if err != nil {
		return err
	}
	hostsconf.Close()
	defer os.Remove(hostsconf.Name())

	optsfile, err := ioutil.TempFile("", "dnsmasq-opts")
	if err != nil {
		return err
	}
	optsfile.Close()
	defer os.Remove(optsfile.Name())

	cmd, err := launchDnsMasq(optsfile.Name(), hostsconf.Name(), flag.Args())
	if err != nil {
		return err
	}

	first := true
	m.OnChange(1, func(version string) {
		if err := writeDnsMasq(optsfile.Name(), hostsconf.Name(), host.UUID, m); err != nil {
			log.Printf("Failed to generate config: %v", err)
			return
		}

		fmt.Println("First: ", first)
		if !first {
			log.Print("Sending SIGHUP to dnsmasq")
			if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
				log.Printf("Error sending SIGHUP: %v", err)
			}
		}
		first = false
	})

	return nil
}

func launchDnsMasq(opts, hosts string, extraArgs []string) (*exec.Cmd, error) {
	args := []string{"-d", "--dhcp-range=10.42.0.1,static",
		fmt.Sprintf("--dhcp-hostsfile=%s", hosts),
		fmt.Sprintf("--dhcp-optsfile=%s", opts)}
	args = append(args, extraArgs...)

	log.Print("Running dnsmasq ", args)

	cmd := exec.Command("dnsmasq", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	go func() {
		err := cmd.Wait()
		log.Fatalf("dnsmasq exited: %v", err)
	}()

	return cmd, nil
}

func writeDnsMasq(optsfile, hostsfile, host string, client *metadata.Client) error {
	containers, err := client.GetContainers()
	if err != nil {
		return err
	}

	var gateway interface{}

	hosts := &bytes.Buffer{}
	opts := &bytes.Buffer{}

	for _, cont := range containers {
		if cont.HostUUID != host {
			continue
		}

		metadata, ok := cont.Labels["io.rancher.vm.metadata"]
		if !ok {
			continue
		}

		data := map[string]interface{}{}
		if err := json.Unmarshal([]byte(metadata), &data); err != nil {
			continue
		}

		mac := data["mac"].(string)
		if len(mac) > 3 {
			mac = "04" + mac[2:]
		}
		ip := data["local-ipv4"]
		hostname := data["hostname"]
		gateway = data["local-ipv4-gateway"]

		fmt.Fprintf(hosts, "%v,%v,%v,infinite\n", mac, hostname, ip)
	}

	// TODO: Make these values more proper.  per host, calculate netmask, get DNS conf from container
	fmt.Fprintf(opts, "option:router,%v\n", gateway)
	fmt.Fprintf(opts, "option:netmask,255.255.0.0\n")
	fmt.Fprintf(opts, "option:dns-server,%s\n", gateway)

	log.Printf("New Configuration\n%s%s", hosts, opts)

	if err := ioutil.WriteFile(hostsfile, hosts.Bytes(), 0600); err != nil {
		return err
	}

	return ioutil.WriteFile(optsfile, opts.Bytes(), 0600)
}
