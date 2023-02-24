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

type localWriter struct {
	buf      []byte
	procName string
	stream   string
}

func indexOf(p []byte, q byte) int {
	n := len(p)
	for i := 0; i < n; i++ {
		if p[i] == '\n' {
			return i
		}
	}
	return -1
}

func (w *localWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		pos := indexOf(w.buf, '\n')
		if pos < 0 {
			break
		}

		var m map[string]any
		err := json.Unmarshal(w.buf[0:pos], &m)
		if err != nil {
			m = map[string]any{
				"msg": string(w.buf[0:pos]),
			}
		}
		m["visorProc"] = w.procName
		m["visorStream"] = w.stream
		m["t"] = time.Now().Format(time.RFC3339)
		ss, err := json.Marshal(m)
		if err != nil {
			panic(err)
		}
		os.Stdout.WriteString(string(ss) + "\n")

		rest := w.buf[pos+1:]
		ll := len(rest)
		copy(w.buf, rest)
		w.buf = w.buf[0:ll]
	}
	return len(p), nil
}

func run(p proc, reboot <-chan bool) {
	args := strings.Split(p.Command, " ")
	quits := 0
	run := func() {
		t := time.Now()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = p.Dir
		cmd.Stdout = &localWriter{procName: p.Name, stream: "stdout"}
		cmd.Stderr = &localWriter{procName: p.Name, stream: "stderr"}
		err := cmd.Start()
		if err != nil {
			return
		}
		msg("info", fmt.Sprintf("started %s, pid = %v\n", p.Name, cmd.Process.Pid), nil)

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

func msg(level string, message string, data map[string]string) {
	e := map[string]string{}
	for k, v := range data {
		e[k] = v
	}
	e["level"] = level
	e["msg"] = message
	e["t"] = time.Now().Format(time.RFC3339)
	s, err := json.Marshal(e)
	if err != nil {
		log.Println("failed to format a log", err)
	}
	os.Stdout.WriteString(string(s) + "\n")
}
