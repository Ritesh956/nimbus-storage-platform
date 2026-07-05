// Package mail is a minimal SMTP sender for transactional email (password
// reset, backlog #14). The dev/demo target is Mailpit, which accepts
// unauthenticated SMTP on the compose network — no AUTH/TLS support until
// a real provider needs it.
package mail

import (
	"net/smtp"
	"strings"
)

type SMTPSender struct {
	addr string // host:port
	from string // bare address — used as both envelope sender and From header
}

func NewSMTPSender(addr, from string) *SMTPSender {
	return &SMTPSender{addr: addr, from: from}
}

func (s *SMTPSender) Send(to, subject, body string) error {
	msg := strings.Join([]string{
		"From: " + s.from,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		body,
	}, "\r\n")
	return smtp.SendMail(s.addr, nil, s.from, []string{to}, []byte(msg))
}
