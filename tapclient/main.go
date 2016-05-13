package main

import (
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/ftrvxmtrx/fd"
	flag "github.com/ogier/pflag"
)

var sock = flag.String("socket", "/var/run/rancher/tap.sock", "Socket to use")
var iface = flag.String("iface", "eth0", "Interface to get mac from")
var mac = flag.String("mac", "", "Mac address to request")

func main() {
	log.Fatal(run())
}

func run() error {
	flag.Parse()

	conn, err := net.Dial("unix", *sock)
	if err != nil {
		return err
	}
	unixConn := conn.(*net.UnixConn)

	addr := *mac
	if addr == "" {
		i, err := net.InterfaceByName(*iface)
		if err != nil {
			return err
		}

		addr = i.HardwareAddr.String()
	}

	addr = "06" + addr[2:]

	log.Printf("Requesting MAC address %s", addr)

	if _, err := unixConn.Write([]byte(addr)); err != nil {
		return err
	}

	files, err := fd.Get(unixConn, 1, []string{"tap"})
	if err != nil {
		return err
	}

	log.Printf("Got FD: %d", files[0].Fd())

	args := flag.Args()
	fdNum := strconv.Itoa(int(files[0].Fd()))
	for i := range args {
		args[i] = strings.Replace(args[i], "%FD%", fdNum, -1)
	}

	if len(args) > 0 {
		bin := args[0]
		if _, err := os.Stat(bin); err != nil {
			bin, err = exec.LookPath(bin)
			if err != nil {
				return err
			}
		}
		return syscall.Exec(bin, args, os.Environ())
	}

	return nil
}
