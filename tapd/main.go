package main

import (
	"log"
	"math/rand"
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
	rand.Seed(time.Now().UnixNano())
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

func randomMac() net.HardwareAddr {
	hw := make(net.HardwareAddr, 6)
	hw[0] = 0x02
	hw[1] = 0x42
	rand.Read(hw[2:])
	return hw
}

func serve(conn *net.UnixConn) error {
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	hw := randomMac()

	log.Printf("Creating tap device for %s", hw.String())

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

	if err := netlink.LinkSetHardwareAddr(link, randomMac()); err != nil {
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
