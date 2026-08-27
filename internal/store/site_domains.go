package store

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// AddSiteDomain menambahkan domain alias untuk situs (siteID).
// Domain dinormalisasi (trim + lowercase), divalidasi, dan dicek tidak bentrok
// baik dengan domain utama situs (tabel sites) maupun alias yang sudah ada
// (tabel site_domains). Race safety: UNIQUE violation saat INSERT pun dipetakan
// ke pesan "sudah terdaftar" yang sama.
func (s *Store) AddSiteDomain(siteID int64, domain string) error {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if err := ValidateDomain(domain); err != nil {
		return err
	}
	var exists int
	err := s.db.QueryRow(
		`SELECT 1 FROM sites WHERE domain=? UNION SELECT 1 FROM site_domains WHERE domain=?`,
		domain, domain,
	).Scan(&exists)
	if err == nil {
		return fmt.Errorf("domain %s sudah terdaftar", domain)
	}
	// err != nil: hanya sql.ErrNoRows (domain belum ada) yang boleh lanjut ke
	// INSERT. Error lain (DB terkunci, I/O) harus dikembalikan, bukan ditelan.
	if err != sql.ErrNoRows {
		return err
	}

	_, err = s.db.Exec(
		`INSERT INTO site_domains(site_id, domain) VALUES(?, ?)`,
		siteID, domain,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return fmt.Errorf("domain %s sudah terdaftar", domain)
		}
		return err
	}
	return nil
}

// ListSiteDomains mengembalikan semua domain alias milik situs, diurutkan
// secara ascending.
func (s *Store) ListSiteDomains(siteID int64) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT domain FROM site_domains WHERE site_id=? ORDER BY domain ASC`,
		siteID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeleteSiteDomain menghapus satu alias (domain) dari situs (siteID).
// Bila baris tidak ditemukan, fungsi mengembalikan nil (no-op, bukan error).
func (s *Store) DeleteSiteDomain(siteID int64, domain string) error {
	domain = strings.TrimSpace(strings.ToLower(domain))
	_, err := s.db.Exec(
		`DELETE FROM site_domains WHERE site_id=? AND domain=?`,
		siteID, domain,
	)
	return err
}

// AllSiteDomains mengembalikan semua alias dikelompokkan per site_id dalam satu
// query. Tiap slice diurutkan secara ascending.
func (s *Store) AllSiteDomains() (map[int64][]string, error) {
	rows, err := s.db.Query(`SELECT site_id, domain FROM site_domains ORDER BY domain ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64][]string)
	for rows.Next() {
		var siteID int64
		var d string
		if err := rows.Scan(&siteID, &d); err != nil {
			return nil, err
		}
		out[siteID] = append(out[siteID], d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Slice sudah terurut karena query ORDER BY domain ASC; sort tambahan hanya
	// pengaman bila urutan GROUP tidak deterministik lintas driver.
	for k := range out {
		sort.Strings(out[k])
	}
	return out, nil
}
