package main

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/mailjet/mailjet-apiv3-go/v3"
)

var qlock sync.Mutex
var outbox = []string{}

func report(msg string) {
	qlock.Lock()
	defer qlock.Unlock()
	outbox = append(outbox, msg)
}

func sendReports(ml *Mailer) {
	for range time.Tick(time.Minute) {
		//
		// Remove messages from the outbox.
		//
		qlock.Lock()
		var report strings.Builder
		for i, s := range outbox {
			fmt.Fprintf(&report, "#%d: %s\n\n", i+1, s)
		}
		outbox = []string{}
		qlock.Unlock()

		//
		// Send if there is something.
		//
		if s := report.String(); s != "" {
			ml.send(s)
		}
	}
}

type Mailer struct {
	from string
	to   string
	mj   *mailjet.Client
	log  *slog.Logger
}

func (m *Mailer) send(msg string) {
	messages := mailjet.MessagesV31{Info: []mailjet.InfoMessagesV31{
		{
			From: &mailjet.RecipientV31{
				Email: m.from,
				Name:  "visor",
			},
			To: &mailjet.RecipientsV31{
				mailjet.RecipientV31{
					Email: m.to,
					Name:  "",
				},
			},
			Subject:  "Visor report",
			TextPart: msg,
		},
	}}
	_, err := m.mj.SendMailV31(&messages)
	if err != nil {
		m.log.Error(fmt.Sprintf("failed to send email: %v", err))
	} else {
		m.log.Info("sent email")
	}
}
