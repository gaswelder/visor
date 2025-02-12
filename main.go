package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
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
	logger  *slog.Logger
}

func main() {
	godotenv.Load()
	data, err := os.ReadFile("visor.json")
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
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == "time" {
				a.Key = "t"
			}
			return a
		},
	}))
	go sendReports(logger, email, key, secret)

	requestChannels := map[string]chan string{}
	quitChannels := map[string]chan bool{}
	for _, p := range config.Processes {
		p.logger = logger.With("visorProc", p.Name)
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
	cmd.Stdout = &localWriter{logger: p.logger}
	cmd.Stderr = &localWriter{logger: p.logger, isStderr: true}
	err := cmd.Start()
	if err != nil {
		return nil, err
	}
	p.logger.Info("started", "visorPid", cmd.Process.Pid)

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
	for {
		child, err := createProcess(p)
		if err != nil {
			p.logger.Error(fmt.Sprintf("failed to start: %v", err))
			return
		}
		select {
		case err := <-child.quitChan:
			report(p.logger, "%s quit after %v: %v", p.Name, time.Since(child.startTime), err)
			quits++
		case req := <-requests:
			switch req {
			case "reboot":
				p.logger.Info("got a reboot signal, waiting for the process to exit")
				child.stop()
				quits = 0
			case "term":
				p.logger.Info("got a termination signal, closing the process")
				child.stop()
				return
			}
		}
		if quits > 2 {
			quits = 0
			report(p.logger, "%s: taking a timeout, %s", p.Name, time.Hour.String())
			select {
			case <-time.After(time.Hour):
			case req := <-requests:
				switch req {
				case "term":
					p.logger.Info("got a termination signal, quitting")
					return
				}
			}
		}
	}
}
