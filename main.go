package main

import (
	"bufio"
	"bytes"
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

	requestChannels := map[string]chan string{}
	quitChannels := map[string]chan bool{}
	for _, p := range config.Processes {
		r := make(chan string)
		q := make(chan bool)
		requestChannels[p.Name] = r
		quitChannels[p.Name] = q
		go maintainProcess(p, requestChannels[p.Name], q)
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
			switch parts[0] {
			case "reboot":
				if len(parts) != 2 {
					ln.Write([]byte("unknown syntax\n"))
					continue
				}
				name := parts[1]
				ch := requestChannels[name]
				if ch == nil {
					ln.Write([]byte("no task named " + name + "\n"))
					break
				}
				ch <- "reboot"
				ln.Write([]byte("ok\n"))
			case "term":
				if len(parts) != 1 {
					ln.Write([]byte("unknown syntax\n"))
					continue
				}
				ln.Write([]byte("ok\n"))
				for k, ch := range requestChannels {
					ch <- "term"
					<-quitChannels[k]
				}
				os.Exit(1)
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

func (w *localWriter) Write(p []byte) (int, error) {
	logData := map[string]any{"visorProc": w.procName}

	// Assume anything that's in stderr is an error and not structured.
	if w.stream == "stderr" {
		logmsg("error", strings.Trim(string(p), "\n"), logData)
		return len(p), nil
	}

	w.buf = append(w.buf, p...)
	for {
		pos := bytes.IndexByte(w.buf, '\n')
		if pos < 0 {
			break
		}

		var m map[string]any
		err := json.Unmarshal(w.buf[0:pos], &m)
		if err != nil {
			// The line is not a JSON, so create a map and put the line there.
			logmsg("warn", string(w.buf[0:pos]), logData)
		} else {
			level, _ := m["level"].(string)
			msg, _ := m["msg"].(string)
			logmsg(level, msg, m)
		}
		rest := w.buf[pos+1:]
		ll := len(rest)
		copy(w.buf, rest)
		w.buf = w.buf[0:ll]
	}
	return len(p), nil
}

type child struct {
	startTime time.Time
	quitChan  chan error
	stop      func()
}

func createProcess(p proc) (*child, error) {
	args := strings.Split(p.Command, " ")

	t := time.Now()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = p.Dir
	cmd.Stdout = &localWriter{procName: p.Name, stream: "stdout"}
	cmd.Stderr = &localWriter{procName: p.Name, stream: "stderr"}
	err := cmd.Start()
	if err != nil {
		return nil, err
	}
	logmsg("info", "started", map[string]any{
		"pid":       cmd.Process.Pid,
		"visorProc": p.Name,
	})
	quit := make(chan error, 1)
	go func() {
		quit <- cmd.Wait()
	}()
	stop := func() {
		cmd.Process.Signal(os.Interrupt)
		err = cmd.Wait()
		if err != nil {
			log.Printf("wait failed: %v", err)
		}
	}
	return &child{t, quit, stop}, nil
}

func maintainProcess(p proc, requests <-chan string, quit chan<- bool) {
	defer func() {
		quit <- true
	}()
	quits := 0
	logData := map[string]any{
		"visorProc": p.Name,
	}
	for {
		child, err := createProcess(p)
		if err != nil {
			logmsg("error", fmt.Sprintf("failed to start: %v", err), logData)
			return
		}
		select {
		case err := <-child.quitChan:
			report("%s quit after %v: %v", p.Name, time.Since(child.startTime), err)
			quits++
		case req := <-requests:
			switch req {
			case "reboot":
				logmsg("info", "got a reboot signal, waiting for the process to exit", logData)
				child.stop()
				quits = 0
			case "term":
				logmsg("info", "got a termination signal, closing the process", logData)
				child.stop()
				return
			}
		}
		if quits > 2 {
			quits = 0
			report("%s: taking a timeout, %s", p.Name, time.Hour.String())
			select {
			case <-time.After(time.Hour):
			case req := <-requests:
				switch req {
				case "term":
					logmsg("info", "got a termination signal, quitting", logData)
					return
				}
			}
		}
	}
}

func logmsg(level, message string, data map[string]any) {
	if _, ok := data["alert"]; ok {
		b, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			report("%s: %v", message, data)
		} else {
			report("%s: %s", message, string(b))
		}
	}
	e := map[string]any{}
	for k, v := range data {
		e[k] = v
	}
	e["level"] = level
	e["msg"] = message
	e["t"] = time.Now().Format(time.RFC3339)
	s, err := json.Marshal(e)
	if err != nil {
		log.Println("failed to format a log", err)
		log.Println(level, message, data)
		return
	}
	os.Stdout.WriteString(string(s) + "\n")
}
