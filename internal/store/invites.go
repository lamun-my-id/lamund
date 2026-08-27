package store

import (
	"database/sql"
	"errors"
)

// Invite adalah undangan bergabung ke tim via token link (sekali-pakai + kadaluarsa).
type Invite struct {
	Token     string
	TeamID    int64
	Role      string
	ExpiresAt string // RFC3339 UTC; '' = tidak ada batas (undangan lama sebelum hardening)
	Email     string // '' = link invite tanpa binding; non-'' = email-invite (hanya untuk penerima ini)
}

func (s *Store) CreateInvite(teamID int64, role, token, expiresAt, email string) error {
	if role != "member" && role != "admin" {
		role = "member"
	}
	_, err := s.db.Exec(
		`INSERT INTO invites(token, team_id, role, expires_at, email) VALUES(?,?,?,?,?)`,
		token, teamID, role, expiresAt, email,
	)
	return err
}

func (s *Store) GetInvite(token string) (*Invite, error) {
	row := s.db.QueryRow(
		`SELECT token, team_id, role, expires_at, email FROM invites WHERE token=?`, token,
	)
	var i Invite
	if err := row.Scan(&i.Token, &i.TeamID, &i.Role, &i.ExpiresAt, &i.Email); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &i, nil
}

func (s *Store) DeleteInvite(token string) error {
	_, err := s.db.Exec(`DELETE FROM invites WHERE token=?`, token)
	return err
}
