package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type job struct {
	m         sync.Mutex
	cfg       CfgProg   // config as is
	isReboot  bool      // whether a reboot is in progress
	cmd       *exec.Cmd // current process instance
	startTime time.Time // current process start time
	logger    *slog.Logger
}

func (j *job) reboot() {
	if j.cmd == nil {
		j.logger.Error("got reboot but not process")
		return
	}
	j.isReboot = true
	j.cmd.Process.Signal(os.Interrupt)
}

func (j *job) begin() {
	go func() {
		fails := 0
		for {
			//
			// Start the process.
			//
			args := strings.Split(j.cfg.Command, " ")
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = j.cfg.Dir
			cmd.Stdout = &localWriter{logger: j.logger}
			cmd.Stderr = &localWriter{logger: j.logger, isStderr: true}
			if err := cmd.Start(); err != nil {
				j.logger.Error(fmt.Sprintf("failed to start: %s", err.Error()))
				return
			}
			j.cmd = cmd
			j.logger.Info("started", "pid", cmd.Process.Pid)
			j.startTime = time.Now()

			//
			// Wait for exit.
			//
			err := cmd.Wait()
			j.m.Lock()

			//
			// If it's a reboot, reset state and proceed directly to restart.
			//
			if j.isReboot {
				j.isReboot = false
				fails = 0
				j.m.Unlock()
				j.logger.Info(fmt.Sprintf("reboot, err=%s", err.Error()))
				continue
			}

			//
			// If actual crash, backoff or exit.
			//
			uptime := time.Since(j.startTime)
			if err != nil {
				j.logger.Error(fmt.Sprintf("exit, err=%s", err.Error()), "uptime", uptime)
			} else {
				j.logger.Error("exit", "uptime", uptime)
			}

			report(fmt.Sprintf("%s quit after %v: %v", j.cfg.Name, uptime, err))
			fails++
			if fails > 2 {
				j.logger.Error(fmt.Sprintf("stopping after %d fails", fails))
				j.m.Unlock()
				return
			}
			j.m.Unlock()
			time.Sleep(time.Minute)
		}
	}()
}
