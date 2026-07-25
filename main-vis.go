package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/mailjet/mailjet-apiv3-go/v3"
)

var jobs []*job

func mainVis() {
	godotenv.Load()
	config, err := parseConfig()
	if err != nil {
		log.Fatal(err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == "time" {
				a.Key = "t"
			}
			return a
		},
	}))

	ml := &Mailer{
		from: config.AdminEmail,
		to:   config.AdminEmail,
		mj:   mailjet.NewMailjetClient(config.mailerKey, config.mailerSecret),
		log:  logger,
	}

	go sendReports(ml)

	for _, p := range config.Processes {
		j := &job{cfg: p, logger: logger.With("visorProc", p.Name)}
		jobs = append(jobs, j)
		j.begin()
	}

	processCommands()
}

func find(name string) *job {
	for _, j := range jobs {
		if j.cfg.Name == name {
			return j
		}
	}
	return nil
}

func processCommands() {
	conn, err := net.Listen("tcp", LocalAddr)
	if err != nil {
		log.Fatal(err)
	}
	for {
		ln, err := conn.Accept()
		if err != nil {
			log.Printf("accept error: %v", err)
			continue
		}
		b := bufio.NewReader(ln)
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

			j := find(name)
			if j == nil {
				ln.Write([]byte("no task named " + name + "\n"))
				break
			}
			j.reboot()
			ln.Write([]byte("ok\n"))
		case "ps":
			for _, j := range jobs {
				cmd := j.cmd
				if cmd == nil {
					fmt.Fprintf(ln, "%s: not running\n", j.cfg.Name)
					continue
				}
				fmt.Fprintf(ln, "%s: pid=%d uptime=%v\n", j.cfg.Name, j.cmd.Process.Pid, time.Since(j.startTime))
			}

		default:
			ln.Write([]byte("unknown command\n"))
		}
		ln.Close()
	}
}
