package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/mailjet/mailjet-apiv3-go/v3"
)

var qlock sync.Mutex
var q = []string{}

func report(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	logmsg("info", "report", map[string]any{
		"report": msg,
	})
	qlock.Lock()
	defer qlock.Unlock()
	q = append(q, msg)
}

func sendReports(email, key, secret string) {
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
			log.Printf("failed to send email: %v", err)
		}
	}
}
