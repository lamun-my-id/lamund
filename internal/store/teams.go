package store

import (
	"database/sql"
	"fmt"
)

// Team = unit kepemilikan bersama di atas user personal. org_id disisakan
// nullable untuk milestone Org/Department kelak (belum dipakai).
type Team struct {
	ID        int64
	Name      string
	Slug      string
	CreatedAt string
}

// TeamMember = keanggotaan user di team dengan peran (owner|admin|member).
type TeamMember struct {
	TeamID   int64
	UserID   int64
	Role     string
	Username string // di-join dari users untuk tampilan
	Name     string
}

func (s *Store) CreateTeam(name, slug string) (Team, error) {
	t := Team{Name: name, Slug: slug}
	id, err := s.db.insertID(`INSERT INTO teams(name, slug) VALUES(?,?)`, name, slug)
	if err != nil {
		if isUniqueErr(err) {
			return t, fmt.Errorf("slug tim %q sudah dipakai", slug)
		}
		return t, err
	}
	t.ID = id
	return t, nil
}

func scanTeam(row interface{ Scan(...any) error }) (*Team, error) {
	var t Team
	err := row.Scan(&t.ID, &t.Name, &t.Slug, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) GetTeam(id int64) (*Team, error) {
	return scanTeam(s.db.QueryRow(`SELECT id, name, slug, created_at FROM teams WHERE id=?`, id))
}

func (s *Store) ListTeams() ([]Team, error) {
	rows, err := s.db.Query(`SELECT id, name, slug, created_at FROM teams ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Team
	for rows.Next() {
		var t Team
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListTeamsByUser mengembalikan tim tempat user jadi anggota, beserta perannya.
func (s *Store) ListTeamsByUser(userID int64) ([]struct {
	Team
	Role string
}, error) {
	rows, err := s.db.Query(`SELECT t.id, t.name, t.slug, t.created_at, m.role
		FROM teams t JOIN team_members m ON m.team_id=t.id
		WHERE m.user_id=? ORDER BY t.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		Team
		Role string
	}
	for rows.Next() {
		var t Team
		var role string
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &t.CreatedAt, &role); err != nil {
			return nil, err
		}
		out = append(out, struct {
			Team
			Role string
		}{t, role})
	}
	return out, rows.Err()
}

func (s *Store) DeleteTeam(id int64) error {
	if _, err := s.db.Exec(`DELETE FROM team_members WHERE team_id=?`, id); err != nil {
		return err
	}
	res, err := s.db.Exec(`DELETE FROM teams WHERE id=?`, id)
	return affectedID(res, err, id)
}

// AddTeamMember menambah/meng-update peran anggota (idempoten).
func (s *Store) AddTeamMember(teamID, userID int64, role string) error {
	if role != "owner" && role != "admin" && role != "member" {
		role = "member"
	}
	_, err := s.db.Exec(`INSERT INTO team_members(team_id, user_id, role) VALUES(?,?,?)
		ON CONFLICT(team_id, user_id) DO UPDATE SET role=excluded.role`, teamID, userID, role)
	return err
}

func (s *Store) RemoveTeamMember(teamID, userID int64) error {
	_, err := s.db.Exec(`DELETE FROM team_members WHERE team_id=? AND user_id=?`, teamID, userID)
	return err
}

func (s *Store) ListTeamMembers(teamID int64) ([]TeamMember, error) {
	rows, err := s.db.Query(`SELECT m.team_id, m.user_id, m.role, u.username, u.name
		FROM team_members m JOIN users u ON u.id=m.user_id
		WHERE m.team_id=? ORDER BY m.role, u.username`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TeamMember
	for rows.Next() {
		var m TeamMember
		if err := rows.Scan(&m.TeamID, &m.UserID, &m.Role, &m.Username, &m.Name); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CountTeamMembers menghitung jumlah anggota tim.
func (s *Store) CountTeamMembers(teamID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM team_members WHERE team_id=?`, teamID).Scan(&n)
	return n, err
}

// GetMemberRole mengembalikan peran user di team, atau ("", false) bila bukan anggota.
func (s *Store) GetMemberRole(teamID, userID int64) (string, bool) {
	var role string
	err := s.db.QueryRow(`SELECT role FROM team_members WHERE team_id=? AND user_id=?`, teamID, userID).Scan(&role)
	if err != nil {
		return "", false
	}
	return role, true
}

// ---- visibilitas resource berbasis owner ----

// ownerVisibleClause: resource personal user UNION resource tim di mana
// (user adalah owner/admin) ATAU (user member biasa DAN resource.user_id=user).
const ownerVisibleClause = `
	(owner_type='user' AND owner_id=?)
	OR (owner_type='team' AND owner_id IN (
		SELECT team_id FROM team_members WHERE user_id=? AND role IN ('owner','admin')))
	OR (owner_type='team' AND user_id=? AND owner_id IN (
		SELECT team_id FROM team_members WHERE user_id=?))`

// ListSitesVisibleTo = situs personal user + situs tim (owner/admin: semua; member: buatannya saja).
func (s *Store) ListSitesVisibleTo(userID int64) ([]Site, error) {
	return s.scanSites(`SELECT `+siteCols+` FROM sites WHERE `+ownerVisibleClause+` ORDER BY domain`,
		userID, userID, userID, userID)
}

// ListAppsVisibleTo = app personal user + app tim (owner/admin: semua; member: buatannya saja).
func (s *Store) ListAppsVisibleTo(userID int64) ([]App, error) {
	return s.queryApps(`SELECT `+appCols+` FROM apps WHERE `+ownerVisibleClause+` ORDER BY domain`,
		userID, userID, userID, userID)
}

func (s *Store) SetSiteOwner(domain, ownerType string, ownerID int64) error {
	res, err := s.db.Exec(`UPDATE sites SET owner_type=?, owner_id=? WHERE domain=?`, ownerType, ownerID, domain)
	return affected(res, err, domain)
}

func (s *Store) SetAppOwner(domain, ownerType string, ownerID int64) error {
	res, err := s.db.Exec(`UPDATE apps SET owner_type=?, owner_id=? WHERE domain=?`, ownerType, ownerID, domain)
	return affected(res, err, domain)
}
