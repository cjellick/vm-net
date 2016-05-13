package main

import (
	"log"
	"net"
	"os"
	"time"

	"github.com/ftrvxmtrx/fd"
	flag "github.com/ogier/pflag"
	"github.com/rancher/netconf"
	"github.com/songgao/water"
	"github.com/vishvananda/netlink"
)

var sock = flag.String("socket", "/var/run/rancher/tap.sock", "Socket to use")
var bridge = flag.String("bridge", "docker0", "Bridge to add tap devices to")

func main() {
	log.Fatal(run())
}

func run() error {
	flag.Parse()

	os.Remove(*sock)

	listener, err := net.Listen("unix", *sock)
	if err != nil {
		return err
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}

		unixConn := conn.(*net.UnixConn)
		go func() {
			if err := serve(unixConn); err != nil {
				log.Printf("Error handling connection: %v", err)
			}
		}()
	}
}

func serve(conn *net.UnixConn) error {
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	buf := make([]byte, 17)
	_, err := conn.Read(buf)
	if err != nil {
		return err
	}

	log.Printf("Creating tap device for %s", buf)
	addr, err := net.ParseMAC(string(buf))
	if err != nil {
		return err
	}

	iface, err := water.NewTAP("")
	if err != nil {
		return err
	}

	f := iface.ReadWriteCloser.(*os.File)
	defer f.Close()

	link, err := netlink.LinkByName(iface.Name())
	if err != nil {
		return err
	}

	if err := netlink.LinkSetHardwareAddr(link, addr); err != nil {
		return err
	}

	bridge, err := netconf.NewBridge(*bridge)
	if err != nil {
		return err
	}

	if err := bridge.AddLink(link); err != nil {
		return err
	}

	if err := netlink.LinkSetUp(link); err != nil {
		return err
	}

	return fd.Put(conn, f)
}
