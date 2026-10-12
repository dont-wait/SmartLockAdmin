package mailer

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"smartlockadmin/server/internal/config"
	"smartlockadmin/server/internal/otp"
)

func Send(cfg config.Config, record otp.Record) error {
	if cfg.SMTPUser == "" || cfg.SMTPPass == "" || cfg.SMTPFrom == "" {
		return fmt.Errorf("SMTP_USER, SMTP_PASS and SMTP_FROM must be configured")
	}
	address := net.JoinHostPort(cfg.SMTPHost, fmt.Sprint(cfg.SMTPPort))
	var conn net.Conn
	var err error
	if cfg.SMTPSecure {
		conn, err = tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", address, &tls.Config{ServerName: cfg.SMTPHost, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = net.DialTimeout("tcp", address, 10*time.Second)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, cfg.SMTPHost)
	if err != nil {
		return err
	}
	defer client.Close()
	if err = client.Auth(smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPHost)); err != nil {
		return err
	}
	if err = client.Mail(cfg.SMTPFrom); err != nil {
		return err
	}
	if err = client.Rcpt(record.Email); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	text := fmt.Sprintf("Your Smart Lock OTP is %s. It expires in 5 minutes.", record.Code)
	message := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", cfg.SMTPFrom, record.Email, strings.ReplaceAll(cfg.EmailSubject, "\r", ""), text)
	if _, err = w.Write([]byte(message)); err != nil {
		return err
	}
	return w.Close()
}
