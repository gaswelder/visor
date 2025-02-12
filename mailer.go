package main

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mailjet/mailjet-apiv3-go/v3"
)

var qlock sync.Mutex
var q = []string{}

func report(log *slog.Logger, format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	log.Info("sending a report", "report", msg)
	qlock.Lock()
	defer qlock.Unlock()
	q = append(q, msg)
}

func sendReports(log *slog.Logger, email, key, secret string) {
	mailjetClient := mailjet.NewMailjetClient(key, secret)
	for range time.Tick(time.Minute) {
		qlock.Lock()
		report := ""
		for i, s := range q {
			report += fmt.Sprintf("#%d: %s\n\n", i+1, s)
		}
		q = []string{}
		qlock.Unlock()

		if report == "" {
			continue
		}

		messages := mailjet.MessagesV31{Info: []mailjet.InfoMessagesV31{
			{
				From: &mailjet.RecipientV31{
					Email: email,
					Name:  "visor",
				},
				To: &mailjet.RecipientsV31{
					mailjet.RecipientV31{
						Email: email,
						Name:  "",
					},
				},
				Subject:  "Visor report",
				TextPart: report,
			},
		}}
		_, err := mailjetClient.SendMailV31(&messages)
		if err != nil {
			log.Error(fmt.Sprintf("failed to send email: %v", err))
		} else {
			log.Info("sent an email")
		}
	}
}
