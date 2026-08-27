package store

import "database/sql"

// Connection = akun eksternal terhubung milik seorang user (GitHub sekarang;
// token AI provider menyusul). token disimpan di DB (dir data 0700) dan TIDAK
// pernah dikembalikan ke klien — hanya dipakai server-side.
type Connection struct {
	Provider  string
	Token     string
	Meta      string // JSON bebas (mis. {"login":"user"} untuk GitHub)
	CreatedAt string
}

// SetConnection menambah/meng-update koneksi provider milik user (idempoten).
func (s *Store) SetConnection(userID int64, provider, token, meta string) error {
	_, err := s.db.Exec(`INSERT INTO connections(user_id, provider, token, meta) VALUES(?,?,?,?)
		ON CONFLICT(user_id, provider) DO UPDATE SET token=excluded.token, meta=excluded.meta`,
		userID, provider, token, meta)
	return err
}

// GetConnection mengembalikan koneksi (termasuk token) untuk pemakaian server-side.
func (s *Store) GetConnection(userID int64, provider string) (*Connection, error) {
	row := s.db.QueryRow(`SELECT provider, token, meta, created_at FROM connections
		WHERE user_id=? AND provider=?`, userID, provider)
	var c Connection
	err := row.Scan(&c.Provider, &c.Token, &c.Meta, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ListConnections mengembalikan koneksi user TANPA token (aman untuk panel).
func (s *Store) ListConnections(userID int64) ([]Connection, error) {
	rows, err := s.db.Query(`SELECT provider, meta, created_at FROM connections
		WHERE user_id=? ORDER BY provider`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Connection
	for rows.Next() {
		var c Connection
		if err := rows.Scan(&c.Provider, &c.Meta, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) DeleteConnection(userID int64, provider string) error {
	_, err := s.db.Exec(`DELETE FROM connections WHERE user_id=? AND provider=?`, userID, provider)
	return err
}
