package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
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
	for _, p := range config.Processes {
		go run(p)
	}
	select {}
}

func run(p proc) {
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
		err = cmd.Wait()
		report("%s quit after %v: %v", p.Name, time.Since(t), err)
	}

	for {
		run()
		quits++
		if quits > 2 {
			quits = 0
			report("%s: taking a timeout, %s", p.Name, time.Hour.String())
			time.Sleep(time.Hour)
		}
	}
}
