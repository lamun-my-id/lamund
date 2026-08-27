// Package email mengirim email transaksional (undangan, reset sandi) lewat
// backend colok-ganti: SMTP/self-hosted (utama), Lamun Mail (HTTP), atau off.
package email

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/lamun-my-id/lamund/internal/store"
)

// Mailer mengirim satu email HTML.
type Mailer interface {
	Send(to, subject, htmlBody string) error
}

// Manager membaca setelan tiap kirim (setelan bisa berubah runtime oleh superadmin).
type Manager struct {
	get    func() (store.EmailSettings, error)
	client *http.Client
}

func NewManager(get func() (store.EmailSettings, error)) *Manager {
	return &Manager{get: get, client: &http.Client{Timeout: 15 * time.Second}}
}

func (m *Manager) Send(to, subject, htmlBody string) error {
	s, err := m.get()
	if err != nil {
		return err
	}
	switch s.Backend {
	case "smtp":
		return sendSMTP(s, to, subject, htmlBody)
	case "lamunmail":
		return m.sendLamunMail(s, to, subject, htmlBody)
	case "off", "":
		return fmt.Errorf("email dinonaktifkan")
	default:
		return fmt.Errorf("backend email tak dikenal: %s", s.Backend)
	}
}

func sendSMTP(s store.EmailSettings, to, subject, htmlBody string) error {
	if s.Host == "" || s.From == "" {
		return fmt.Errorf("SMTP belum dikonfigurasi (host/from)")
	}
	addr := s.Host + ":" + strconv.Itoa(s.Port)
	var auth smtp.Auth
	if s.Username != "" {
		auth = smtp.PlainAuth("", s.Username, s.Password, s.Host)
	}
	msg, err := buildMIME(s.From, to, subject, htmlBody)
	if err != nil {
		return err
	}
	return smtp.SendMail(addr, auth, s.From, []string{to}, msg)
}

func buildMIME(from, to, subject, htmlBody string) ([]byte, error) {
	for _, v := range []string{from, to, subject} {
		if strings.ContainsAny(v, "\r\n") {
			return nil, fmt.Errorf("nilai header email mengandung CR/LF")
		}
	}
	if _, err := mail.ParseAddress(to); err != nil {
		return nil, fmt.Errorf("alamat tujuan tidak valid: %w", err)
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
	b.WriteString(htmlBody)
	return b.Bytes(), nil
}

// sendLamunMail: integrasi generic HTTP ke Lamun Mail. Kontrak: POST {api_base}/send
// Authorization: Bearer {api_key}, body {from,to,subject,html}. TODO: samakan
// dengan kontrak Lamun Mail sebenarnya (kuota/domain ditangani di sisi Lamun Mail).
func (m *Manager) sendLamunMail(s store.EmailSettings, to, subject, htmlBody string) error {
	if s.APIBase == "" || s.APIKey == "" {
		return fmt.Errorf("Lamun Mail belum dikonfigurasi (api_base/api_key)")
	}
	payload, _ := json.Marshal(map[string]string{"from": s.From, "to": to, "subject": subject, "html": htmlBody})
	req, err := http.NewRequest("POST", s.APIBase+"/send", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("URL Lamun Mail tidak valid: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("Lamun Mail menolak: status %d", resp.StatusCode)
	}
	return nil
}
