package main

// Port of mailer.js + the email templates from emailQueue.js.

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/smtp"
	"strings"
	"sync"
	"time"
)

type emailData struct {
	To      string
	Subject string
	Body    string
}

// Build the welcome-email row data; used so the enqueue is atomic with the user create.
func welcomeEmail(to string) emailData {
	return emailData{to, "Your account has been created",
		"Hi,\n\nYou've been successfully added to our system.\n\nThanks,\nThe Team"}
}

// Build the verification-email row data with the click-to-verify link.
func verificationEmail(to, link string) emailData {
	return emailData{to, "Verify your email address",
		fmt.Sprintf("Hi,\n\nPlease verify your email address by visiting this link:\n\n%s\n\nThanks,\nThe Team", link)}
}

// Build the password-reset email row data with the click-to-reset link.
func passwordResetEmail(to, link string) emailData {
	return emailData{to, "Reset your password",
		fmt.Sprintf("Hi,\n\nA password reset was requested for this account. Click the link below to choose a new password (expires in 1 hour):\n\n%s\n\nIf you didn't request this, you can ignore this email.\n\nThanks,\nThe Team", link)}
}

// Next.js client routes (no .html).
func tokenLink(page, token string) string {
	return fmt.Sprintf("%s/%s?token=%s", cfg.FrontendURL, page, token)
}

var logTransportOnce sync.Once

func sendMail(to, subject, text string) error {
	from := cfg.MailFrom
	// ponytail: Real SMTP when SMTP_HOST is set, otherwise the email is
	// logged instead of sent — dev works with no mail server.
	if cfg.SMTPHost == "" {
		logTransportOnce.Do(func() {
			log.Println("[mailer] SMTP_HOST not set — emails are logged, not sent.")
		})
		row, _ := json.Marshal(map[string]string{"from": from, "to": to, "subject": subject, "text": text})
		log.Printf("[mailer] email: %s", row)
		return nil
	}
	if strings.ContainsAny(to+subject+from, "\r\n") {
		return fmt.Errorf("invalid header characters in email")
	}
	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)
	host := cfg.SMTPHost
	var client *smtp.Client
	var err error
	if cfg.SMTPPort == 465 {
		conn, dialErr := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
		if dialErr != nil {
			return dialErr
		}
		client, err = smtp.NewClient(conn, host)
		if err != nil {
			return err
		}
	} else {
		client, err = smtp.Dial(addr)
		if err != nil {
			return err
		}
		// Opportunistic STARTTLS, like nodemailer's default.
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: host}); err != nil {
				client.Close()
				return err
			}
		}
	}
	defer client.Close()

	if cfg.SMTPUser != "" {
		auth := smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPass, host)
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(buildMessage(from, to, subject, text)); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func buildMessage(from, to, subject, text string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(text)
	b.WriteString("\r\n")
	return []byte(b.String())
}
