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
