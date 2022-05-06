package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
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
	quits := 0
	for {
		t := time.Now()
		cmd := exec.Command(p.Command)
		cmd.Dir = p.Dir
		err := cmd.Run()
		dt := time.Since(t)
		quits++
		report("%s quit after %v: %v", p.Name, dt.String(), err)
		if quits > 2 {
			report("%s: taking a timeout, %s", p.Name, time.Hour.String())
			time.Sleep(time.Hour)
			quits = 0
		}
	}
}
