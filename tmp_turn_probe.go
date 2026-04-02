package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/pion/turn/v4"
)

func main() {
	var host string
	var port int
	var username string
	var password string

	flag.StringVar(&host, "host", "8.162.1.176", "TURN host")
	flag.IntVar(&port, "port", 3478, "TURN port")
	flag.StringVar(&username, "username", "", "TURN username")
	flag.StringVar(&password, "password", "", "TURN password")
	flag.Parse()

	if username == "" || password == "" {
		fmt.Fprintln(os.Stderr, "username and password are required")
		os.Exit(2)
	}

	pc, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen packet: %v\n", err)
		os.Exit(1)
	}
	defer pc.Close()

	client, err := turn.NewClient(&turn.ClientConfig{
		STUNServerAddr: net.JoinHostPort(host, fmt.Sprint(port)),
		TURNServerAddr: net.JoinHostPort(host, fmt.Sprint(port)),
		Username:       username,
		Password:       password,
		Realm:          "mobilevc",
		Conn:           pc,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "new client: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	if err := client.Listen(); err != nil {
		fmt.Fprintf(os.Stderr, "listen client: %v\n", err)
		os.Exit(1)
	}

	relayConn, err := client.Allocate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "allocate: %v\n", err)
		os.Exit(1)
	}
	defer relayConn.Close()

	fmt.Printf("relay-local=%s\n", relayConn.LocalAddr())

	peer := &net.UDPAddr{IP: net.ParseIP("1.1.1.1"), Port: 9999}
	_ = relayConn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := relayConn.WriteTo([]byte("probe"), peer); err != nil {
		fmt.Printf("write-to-peer-error=%v\n", err)
	}
}
