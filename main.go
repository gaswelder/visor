package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
)

const LocalAddr = "localhost:1829"

func main() {
	if len(os.Args) > 1 {
		cmd := strings.Join(os.Args[1:], " ") + "\n"
		conn, err := net.Dial("tcp", "localhost:1829")
		if err != nil {
			log.Fatal(err)
		}
		if _, err = conn.Write([]byte(cmd)); err != nil {
			log.Fatal(err)
		}
		line := make([]byte, 4096)
		for {
			n, err := conn.Read(line)
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Fatal(err)
			}
			if n == 0 {
				break
			}
			fmt.Print(string(line[:n]))
		}
		conn.Close()
		os.Exit(0)
	}
	mainVis()
}
