package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

var config struct {
	AdminEmail string
	Processes  []proc
}

type proc struct {
	Name    string
	Command string
	Dir     string
}

func main() {
	godotenv.Load()
	data, err := ioutil.ReadFile("visor.json")
	if err != nil {
		log.Fatal(err)
	}
	if err := json.Unmarshal(data, &config); err != nil {
		log.Fatal(err)
	}
	conn, err := net.Listen("tcp", "localhost:1829")
	if err != nil {
		log.Fatal(err)
	}

	key := os.Getenv("MAILJET_KEY")
	secret := os.Getenv("MAILJET_SECRET")
	if key == "" || secret == "" {
		log.Fatal("Missing MAILJET_KEY or MAILJET_SECRET env variables")
	}
	email := config.AdminEmail
	if email == "" {
		log.Fatal("Missing email config parameter")
	}
	go sendReports(email, key, secret)

	reboots := map[string]chan bool{}
	for _, p := range config.Processes {
		reboots[p.Name] = make(chan bool)
		go run(p, reboots[p.Name])
	}

	for {
		ln, err := conn.Accept()
		if err != nil {
			log.Printf("accept error: %v", err)
			continue
		}
		b := bufio.NewReader(ln)
		for {
			line, err := b.ReadString('\n')
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Printf("read error: %v", err)
				break
			}
			line = strings.Trim(line, " \r\n")
			parts := strings.Split(line, " ")
			if len(parts) != 2 {
				ln.Write([]byte("unknown syntax\n"))
				continue
			}
			switch parts[0] {
			case "reboot":
				name := parts[1]
				ch := reboots[name]
				if ch == nil {
					ln.Write([]byte("no task named " + name + "\n"))
					break
				}
				ch <- true
				ln.Write([]byte("ok\n"))
			default:
				ln.Write([]byte("unknown command\n"))
			}
		}
		ln.Close()
	}
}

func run(p proc, reboot <-chan bool) {
	f, err := os.OpenFile(fmt.Sprintf("%s.log", p.Name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	args := strings.Split(p.Command, " ")
	quits := 0
	run := func() {
		t := time.Now()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = p.Dir
		cmd.Stdout = f
		cmd.Stderr = f
		err := cmd.Start()
		if err != nil {
			return
		}
		log.Printf("started %s, pid = %v\n", p.Name, cmd.Process.Pid)
		quit := make(chan error, 1)
		go func() {
			quit <- cmd.Wait()
		}()
		select {
		case err := <-quit:
			report("%s quit after %v: %v", p.Name, time.Since(t), err)
			quits++
		case <-reboot:
			log.Printf("got a reboot signal for %s, waiting for the process to exit", p.Name)
			cmd.Process.Signal(os.Interrupt)
			err = cmd.Wait()
			if err != nil {
				log.Printf("wait failed: %v", err)
			}
			quits = 0
		}
	}
	for {
		run()
		if quits > 2 {
			quits = 0
			report("%s: taking a timeout, %s", p.Name, time.Hour.String())
			time.Sleep(time.Hour)
		}
	}
}
