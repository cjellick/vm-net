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

const (
	leaseFile = "/var/lib/misc/vm-dnsmasq.leases"
)

var (
	metadataURL = flag.String("metadata", "http://rancher-metadata/2015-12-19", "Metadata URL")
	iface       = flag.String("interface", "eth0", "The interface to expire leases on")
	ipToMac     = map[string]string{}
)

type clientEntry struct {
	mac         string
	hostname    string
	createIndex int
}

func main() {
	flag.Parse()
	log.Fatal(run())
}

func run() error {
	m, err := metadata.NewClientAndWait(*metadataURL)
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

	if err := writeDNSMasq(optsfile.Name(), hostsconf.Name(), host.UUID, m); err != nil {
		log.Printf("Failed to generate config: %v", err)
		return err
	}

	cmd, err := launchDNSMasq(optsfile.Name(), hostsconf.Name(), flag.Args())
	if err != nil {
		return err
	}

	first := true
	m.OnChange(1, func(version string) {
		if err := writeDNSMasq(optsfile.Name(), hostsconf.Name(), host.UUID, m); err != nil {
			log.Printf("Failed to generate config: %v", err)
			return
		}

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

func launchDNSMasq(opts, hosts string, extraArgs []string) (*exec.Cmd, error) {
	args := []string{"-d", "--dhcp-range=10.42.0.1,static",
		fmt.Sprintf("--dhcp-hostsfile=%s", hosts),
		fmt.Sprintf("--dhcp-optsfile=%s", opts),
		fmt.Sprintf("--dhcp-leasefile=%s", leaseFile)}
	args = append(args, extraArgs...)

	err := os.Remove(leaseFile)
	if err == nil {
		log.Printf("Deleted lease file %s", leaseFile)
	}

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

func writeDNSMasq(optsfile, hostsfile, host string, client *metadata.Client) error {
	filtered := map[string]clientEntry{}

	containers, err := client.GetContainers()
	if err != nil {
		return err
	}

	var gateway interface{}

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
		ip := data["local-ipv4"].(string)
		hostname := data["hostname"].(string)
		gateway = data["local-ipv4-gateway"]

		if cont.CreateIndex >= filtered[ip].createIndex {
			filtered[ip] = clientEntry{
				mac:      mac,
				hostname: hostname,
			}
		}
	}

	hosts := &bytes.Buffer{}
	opts := &bytes.Buffer{}

	for ip, entry := range filtered {
		if oldMac, ok := ipToMac[ip]; ok && oldMac != entry.mac {
			log.Printf("Expiring old lease for %s for client %s", ip, oldMac)
			cmd := exec.Command("dhcp_release", *iface, ip, oldMac, "*")
			cmd.Stderr = os.Stderr
			cmd.Stdout = os.Stdout
			if err := cmd.Run(); err != nil {
				return err
			}
		}

		ipToMac[ip] = entry.mac
		fmt.Fprintf(hosts, "%v,%v,%v,infinite\n", entry.mac, entry.hostname, ip)
	}

	// TODO: Make these values more proper.  per host, calculate netmask, get DNS conf from container
	if gateway != nil {
		fmt.Fprintf(opts, "option:router,%v\n", gateway)
		fmt.Fprintf(opts, "option:netmask,255.255.0.0\n")
		fmt.Fprintf(opts, "option:dns-server,%s\n", gateway)
	}

	log.Printf("New Configuration:\n%s%s", hosts, opts)

	if err := ioutil.WriteFile(hostsfile, hosts.Bytes(), 0600); err != nil {
		return err
	}

	return ioutil.WriteFile(optsfile, opts.Bytes(), 0600)
}
