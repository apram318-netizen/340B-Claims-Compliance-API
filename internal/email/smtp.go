package email

import (
	"context"
	"fmt"
	"net/smtp"
	"os"
	"strconv"
	"strings"
)

// Config holds SMTP settings from the environment.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	From     string
}

// ConfigFromEnv returns nil if EMAIL_SMTP_HOST is unset (email disabled).
func ConfigFromEnv() *Config {
	host := strings.TrimSpace(os.Getenv("EMAIL_SMTP_HOST"))
	if host == "" {
		return nil
	}
	port := 587
	if p := strings.TrimSpace(os.Getenv("EMAIL_SMTP_PORT")); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			port = n
		}
	}
	return &Config{
		Host:     host,
		Port:     port,
		User:     strings.TrimSpace(os.Getenv("EMAIL_SMTP_USER")),
		Password: strings.TrimSpace(os.Getenv("EMAIL_SMTP_PASSWORD")),
		From:     strings.TrimSpace(os.Getenv("EMAIL_FROM")),
	}
}

// SMTPMailer sends plain-text email via STARTTLS (typical port 587).
type SMTPMailer struct {
	cfg *Config
}

func NewSMTP(cfg *Config) *SMTPMailer {
	return &SMTPMailer{cfg: cfg}
}

// SendPasswordReset delivers a reset token out-of-band. plaintextToken is never logged here.
func (m *SMTPMailer) SendPasswordReset(ctx context.Context, toEmail, subject, body string) error {
	if m.cfg.From == "" {
		return fmt.Errorf("EMAIL_FROM is required when SMTP is configured")
	}
	addr := fmt.Sprintf("%s:%d", m.cfg.Host, m.cfg.Port)
	msg := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s\r\n",
		m.cfg.From, toEmail, subject, body))

	// net/smtp does not take context cancellation mid-send; caller may wrap with timeout.
	done := make(chan error, 1)
	go func() {
		var auth smtp.Auth
		if m.cfg.User != "" {
			auth = smtp.PlainAuth("", m.cfg.User, m.cfg.Password, m.cfg.Host)
		}
		err := smtp.SendMail(addr, auth, m.cfg.From, []string{toEmail}, msg)
		done <- err
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}
