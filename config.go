package main

import (
	"encoding/json"
	"errors"
	"os"
)

type Cfg struct {
	AdminEmail              string
	Processes               []CfgProg
	mailerKey, mailerSecret string
}

type CfgProg struct {
	Name    string // reference name
	Command string // command to run (cmd arg arg...)
	Dir     string // working directory
}

func parseConfig() (Cfg, error) {
	var config Cfg

	data, err := os.ReadFile("visor.json")
	if err != nil {
		return config, err
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return config, err
	}
	config.mailerKey = os.Getenv("MAILJET_KEY")
	config.mailerSecret = os.Getenv("MAILJET_SECRET")
	if config.mailerKey == "" || config.mailerSecret == "" {
		return config, errors.New("missing MAILJET_KEY or MAILJET_SECRET env variables")
	}
	if config.AdminEmail == "" {
		return config, errors.New("Missing email config parameter")
	}
	return config, nil
}
