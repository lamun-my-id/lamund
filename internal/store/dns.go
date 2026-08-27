package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// DNSZone adalah zona authoritative yang dikelola lamund.
type DNSZone struct {
	ID        int64  `json:"id"`
	Domain    string `json:"domain"`
	OwnerType string `json:"owner_type"`
	OwnerID   int64  `json:"owner_id"`
	UserID    int64  `json:"user_id"`
	Serial    int64  `json:"serial"`
	Refresh   int64  `json:"refresh"`
	Retry     int64  `json:"retry"`
	Expire    int64  `json:"expire"`
	Minimum   int64  `json:"minimum"`
	CreatedAt string `json:"created_at"`
}

// DNSRecord adalah satu resource record di dalam sebuah zona.
type DNSRecord struct {
	ID        int64  `json:"id"`
	ZoneID    int64  `json:"zone_id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Value     string `json:"value"`
	TTL       int64  `json:"ttl"`
	Priority  int64  `json:"priority"`
	CreatedAt string `json:"created_at"`
}

// DNSSettings menyimpan setelan nameserver per-instance.
type DNSSettings struct {
	NS1        string
	NS2        string
	Hostmaster string
}

// zoneCols adalah urutan baku kolom SELECT dns_zones.
const zoneCols = `id, domain, owner_type, owner_id, user_id, serial, refresh, retry, expire, minimum, created_at`

func scanZone(row interface{ Scan(...any) error }) (*DNSZone, error) {
	var z DNSZone
	err := row.Scan(&z.ID, &z.Domain, &z.OwnerType, &z.OwnerID, &z.UserID,
		&z.Serial, &z.Refresh, &z.Retry, &z.Expire, &z.Minimum, &z.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &z, nil
}

// bumpSerial menaikkan serial zona sebesar 1 dalam transaksi yang sedang berjalan.
func bumpSerial(tx *dbTx, zoneID int64) error {
	_, err := tx.Exec(`UPDATE dns_zones SET serial = serial + 1 WHERE id=?`, zoneID)
	return err
}

// GetDNSSettings mengambil setelan DNS singleton (id=1).
func (s *Store) GetDNSSettings() (DNSSettings, error) {
	var d DNSSettings
	err := s.db.QueryRow(`SELECT ns1, ns2, hostmaster FROM dns_settings WHERE id=1`).
		Scan(&d.NS1, &d.NS2, &d.Hostmaster)
	return d, err
}

// SetDNSSettings memperbarui setelan DNS singleton.
func (s *Store) SetDNSSettings(d DNSSettings) error {
	_, err := s.db.Exec(`UPDATE dns_settings SET ns1=?, ns2=?, hostmaster=? WHERE id=1`,
		d.NS1, d.NS2, d.Hostmaster)
	return err
}

// CreateDNSZoneWithSettings membuat zona baru dan (bila NS1/NS2 terisi) menyisipkan
// record NS apex. Ini adalah fungsi inti yang deterministik — dipakai oleh test.
func (s *Store) CreateDNSZoneWithSettings(z DNSZone, settings DNSSettings) (int64, error) {
	z.Domain = strings.ToLower(strings.TrimSpace(z.Domain))
	if err := ValidateDomain(z.Domain); err != nil {
		return 0, err
	}

	// Default serial & SOA fields.
	if z.Serial == 0 {
		z.Serial = 1
	}
	if z.Refresh == 0 {
		z.Refresh = 7200
	}
	if z.Retry == 0 {
		z.Retry = 3600
	}
	if z.Expire == 0 {
		z.Expire = 1209600
	}
	if z.Minimum == 0 {
		z.Minimum = 3600
	}

	// Default owner = pembuat (personal).
	if z.OwnerType == "" {
		z.OwnerType = "user"
		z.OwnerID = z.UserID
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}

	zoneID, err := tx.insertID(
		`INSERT INTO dns_zones(domain, owner_type, owner_id, user_id, serial, refresh, retry, expire, minimum)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		z.Domain, z.OwnerType, z.OwnerID, z.UserID,
		z.Serial, z.Refresh, z.Retry, z.Expire, z.Minimum,
	)
	if err != nil {
		_ = tx.Rollback()
		if isUniqueErr(err) {
			return 0, fmt.Errorf("zona %s sudah terdaftar", z.Domain)
		}
		return 0, err
	}

	// Bootstrap NS records bila nameserver sudah dikonfigurasi.
	if settings.NS1 != "" {
		if _, err := tx.Exec(
			`INSERT INTO dns_records(zone_id, name, type, value, ttl) VALUES(?,?,?,?,?)`,
			zoneID, "@", "NS", settings.NS1, 3600,
		); err != nil {
			_ = tx.Rollback()
			return 0, err
		}
	}
	if settings.NS2 != "" {
		if _, err := tx.Exec(
			`INSERT INTO dns_records(zone_id, name, type, value, ttl) VALUES(?,?,?,?,?)`,
			zoneID, "@", "NS", settings.NS2, 3600,
		); err != nil {
			_ = tx.Rollback()
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return zoneID, nil
}

// CreateDNSZone membuat zona baru dengan membaca setelan DNS global terlebih dahulu.
// Dipakai oleh REST API.
func (s *Store) CreateDNSZone(z DNSZone) (int64, error) {
	settings, err := s.GetDNSSettings()
	if err != nil {
		return 0, err
	}
	return s.CreateDNSZoneWithSettings(z, settings)
}

// GetDNSZone mengambil zona berdasarkan domain; mengembalikan nil bila tidak ada.
func (s *Store) GetDNSZone(domain string) (*DNSZone, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	return scanZone(s.db.QueryRow(`SELECT `+zoneCols+` FROM dns_zones WHERE domain=?`, domain))
}

// ListDNSZones mengembalikan semua zona, diurutkan berdasarkan domain.
func (s *Store) ListDNSZones() ([]DNSZone, error) {
	rows, err := s.db.Query(`SELECT ` + zoneCols + ` FROM dns_zones ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanZones(rows)
}

// ListDNSZonesVisibleTo mengembalikan zona yang dapat diakses oleh userID
// (zona personal + zona tim, sesuai peran — mirror ListSitesVisibleTo).
func (s *Store) ListDNSZonesVisibleTo(userID int64) ([]DNSZone, error) {
	rows, err := s.db.Query(
		`SELECT `+zoneCols+` FROM dns_zones WHERE `+ownerVisibleClause+` ORDER BY domain`,
		userID, userID, userID, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanZones(rows)
}

func scanZones(rows *sql.Rows) ([]DNSZone, error) {
	var out []DNSZone
	for rows.Next() {
		var z DNSZone
		if err := rows.Scan(&z.ID, &z.Domain, &z.OwnerType, &z.OwnerID, &z.UserID,
			&z.Serial, &z.Refresh, &z.Retry, &z.Expire, &z.Minimum, &z.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, z)
	}
	return out, rows.Err()
}

// DeleteDNSZone menghapus zona dan semua record-nya dalam satu transaksi.
// FK enforcement tidak aktif — cascade dilakukan secara eksplisit.
func (s *Store) DeleteDNSZone(domain string) error {
	domain = strings.ToLower(strings.TrimSpace(domain))
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	// Hapus record terlebih dahulu.
	if _, err := tx.Exec(
		`DELETE FROM dns_records WHERE zone_id=(SELECT id FROM dns_zones WHERE domain=?)`,
		domain,
	); err != nil {
		_ = tx.Rollback()
		return err
	}

	res, err := tx.Exec(`DELETE FROM dns_zones WHERE domain=?`, domain)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_ = tx.Rollback()
		return fmt.Errorf("zona %s tidak ditemukan", domain)
	}
	return tx.Commit()
}

// ListDNSRecords mengembalikan semua record pada zona (zoneID), diurutkan name, type.
func (s *Store) ListDNSRecords(zoneID int64) ([]DNSRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, zone_id, name, type, value, ttl, priority, created_at
		 FROM dns_records WHERE zone_id=? ORDER BY name, type`,
		zoneID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DNSRecord
	for rows.Next() {
		var r DNSRecord
		if err := rows.Scan(&r.ID, &r.ZoneID, &r.Name, &r.Type, &r.Value, &r.TTL, &r.Priority, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddDNSRecord menyisipkan record baru dan menaikkan serial zona dalam satu transaksi.
func (s *Store) AddDNSRecord(r DNSRecord) (int64, error) {
	r.Name = strings.ToLower(strings.TrimSpace(r.Name))
	// Tipe DNS dikanonikkan uppercase (A/AAAA/CNAME/...) agar data plane bisa
	// membandingkan tipe secara deterministik tanpa peduli casing input.
	r.Type = strings.ToUpper(strings.TrimSpace(r.Type))

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}

	id, err := tx.insertID(
		`INSERT INTO dns_records(zone_id, name, type, value, ttl, priority) VALUES(?,?,?,?,?,?)`,
		r.ZoneID, r.Name, r.Type, r.Value, r.TTL, r.Priority,
	)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}

	if err := bumpSerial(tx, r.ZoneID); err != nil {
		_ = tx.Rollback()
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// UpdateDNSRecord memperbarui value/ttl/priority sebuah record dan menaikkan serial zona.
func (s *Store) UpdateDNSRecord(zoneID, id int64, value string, ttl, priority int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	res, err := tx.Exec(
		`UPDATE dns_records SET value=?, ttl=?, priority=? WHERE id=? AND zone_id=?`,
		value, ttl, priority, id, zoneID,
	)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_ = tx.Rollback()
		return fmt.Errorf("record %d tidak ditemukan di zona %d", id, zoneID)
	}

	if err := bumpSerial(tx, zoneID); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

// DeleteDNSRecord menghapus record dan menaikkan serial zona dalam satu transaksi.
func (s *Store) DeleteDNSRecord(zoneID, id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	res, err := tx.Exec(`DELETE FROM dns_records WHERE id=? AND zone_id=?`, id, zoneID)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_ = tx.Rollback()
		return fmt.Errorf("record %d tidak ditemukan di zona %d", id, zoneID)
	}

	if err := bumpSerial(tx, zoneID); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

// FindDNSZoneForDomain mencari zona authoritative yang mengelola domain tersebut.
// Normalisasi domain ke lowercase; iterasi semua zona; cocokkan bila domain==apex
// atau merupakan subdomain pada batas label (HasSuffix(domain, "."+apex)).
// Bila beberapa zona cocok, pilih yang apex-nya terpanjang (paling spesifik).
// Kembalikan (zone, label, true) di mana label="@" bila domain==apex,
// selain itu bagian sebelum ".apex" (mis. "blog.x.com" → apex "x.com" → "blog").
// Bila tidak ada yang cocok, kembalikan (nil,"",false).
func (s *Store) FindDNSZoneForDomain(domain string) (*DNSZone, string, bool) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	zones, err := s.ListDNSZones()
	if err != nil {
		return nil, "", false
	}
	var best *DNSZone
	for i := range zones {
		apex := zones[i].Domain
		if domain != apex && !strings.HasSuffix(domain, "."+apex) {
			continue
		}
		if best == nil || len(apex) > len(best.Domain) {
			z := zones[i]
			best = &z
		}
	}
	if best == nil {
		return nil, "", false
	}
	label := "@"
	if domain != best.Domain {
		label = strings.TrimSuffix(domain, "."+best.Domain)
	}
	return best, label, true
}

// DeleteDNSRecordsMatching menghapus semua record yang cocok dengan (zone_id, name, type, value).
// name dinormalisasi lowercase, rtype uppercase. Mengembalikan jumlah baris terhapus.
// Bila tidak ada yang cocok, mengembalikan (0, nil) tanpa error.
// Bila ada yang terhapus, serial zona dinaikkan dalam transaksi yang sama.
func (s *Store) DeleteDNSRecordsMatching(zoneID int64, name, rtype, value string) (int, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	rtype = strings.ToUpper(strings.TrimSpace(rtype))

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}

	res, err := tx.Exec(
		`DELETE FROM dns_records WHERE zone_id=? AND name=? AND type=? AND value=?`,
		zoneID, name, rtype, value,
	)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}

	n, err := res.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}

	if n > 0 {
		if err := bumpSerial(tx, zoneID); err != nil {
			_ = tx.Rollback()
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(n), nil
}

// AddDNSRecordIfAbsent menyisipkan record baru hanya bila belum ada record dengan
// (zone_id, name, type, value) yang identik (name dinormalisasi lowercase, type uppercase).
// Bila sudah ada → kembalikan (false, nil) tanpa insert.
// Bila belum ada → panggil AddDNSRecord dan kembalikan (true, err).
// Fungsi ini menjamin idempoten untuk auto-provisioning.
func (s *Store) AddDNSRecordIfAbsent(r DNSRecord) (bool, error) {
	r.Name = strings.ToLower(strings.TrimSpace(r.Name))
	r.Type = strings.ToUpper(strings.TrimSpace(r.Type))
	var dummy int
	err := s.db.QueryRow(
		`SELECT 1 FROM dns_records WHERE zone_id=? AND name=? AND type=? AND value=?`,
		r.ZoneID, r.Name, r.Type, r.Value,
	).Scan(&dummy)
	if err == nil {
		// Baris ditemukan — lewati insert.
		return false, nil
	}
	if err != sql.ErrNoRows {
		return false, err
	}
	_, err = s.AddDNSRecord(r)
	return true, err
}

// AllDNSData mengambil semua zona, record (diindeks per zone_id), dan setelan DNS
// dalam sedikit query. Dipakai oleh data plane untuk membangun tabel zona in-memory.
func (s *Store) AllDNSData() ([]DNSZone, map[int64][]DNSRecord, DNSSettings, error) {
	zones, err := s.ListDNSZones()
	if err != nil {
		return nil, nil, DNSSettings{}, err
	}

	rows, err := s.db.Query(
		`SELECT id, zone_id, name, type, value, ttl, priority, created_at FROM dns_records ORDER BY zone_id, name, type`,
	)
	if err != nil {
		return nil, nil, DNSSettings{}, err
	}
	defer rows.Close()

	recs := make(map[int64][]DNSRecord)
	for rows.Next() {
		var r DNSRecord
		if err := rows.Scan(&r.ID, &r.ZoneID, &r.Name, &r.Type, &r.Value, &r.TTL, &r.Priority, &r.CreatedAt); err != nil {
			return nil, nil, DNSSettings{}, err
		}
		recs[r.ZoneID] = append(recs[r.ZoneID], r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, DNSSettings{}, err
	}

	settings, err := s.GetDNSSettings()
	if err != nil {
		return nil, nil, DNSSettings{}, err
	}

	return zones, recs, settings, nil
}
