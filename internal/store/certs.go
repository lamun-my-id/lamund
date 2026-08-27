package store

import "database/sql"

// CertInfo mencerminkan status sertifikat sebuah domain (untuk panel & status
// endpoint). certmagic yang memegang cert asli + memperpanjang; tabel ini
// hanya cerminan yang bisa dibaca.
type CertInfo struct {
	Domain          string
	Issuer          string
	NotAfter        string // RFC3339
	Status          string // valid|pending|expiring|error
	LastRenewAttempt string
}

func (s *Store) UpsertCert(c CertInfo) error {
	_, err := s.db.Exec(
		`INSERT INTO certificates(domain, issuer, not_after, status, last_renew_attempt)
		 VALUES(?,?,?,?,?)
		 ON CONFLICT(domain) DO UPDATE SET
		   issuer=excluded.issuer, not_after=excluded.not_after,
		   status=excluded.status, last_renew_attempt=excluded.last_renew_attempt`,
		c.Domain, c.Issuer, c.NotAfter, c.Status, c.LastRenewAttempt)
	return err
}

func (s *Store) GetCert(domain string) (*CertInfo, error) {
	row := s.db.QueryRow(`SELECT domain, issuer, not_after, status, last_renew_attempt
		FROM certificates WHERE domain = ?`, domain)
	var c CertInfo
	err := row.Scan(&c.Domain, &c.Issuer, &c.NotAfter, &c.Status, &c.LastRenewAttempt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) ListCerts() ([]CertInfo, error) {
	rows, err := s.db.Query(`SELECT domain, issuer, not_after, status, last_renew_attempt
		FROM certificates ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CertInfo
	for rows.Next() {
		var c CertInfo
		if err := rows.Scan(&c.Domain, &c.Issuer, &c.NotAfter, &c.Status, &c.LastRenewAttempt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
