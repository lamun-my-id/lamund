package store

// EmailSettings menyimpan konfigurasi pengiriman email instance.
type EmailSettings struct {
	Backend  string // off | smtp | lamunmail
	Host     string
	Port     int
	Username string
	Password string
	From     string
	TLS      bool
	APIBase  string
	APIKey   string
}

// PasswordReset merepresentasikan token reset kata sandi sekali-pakai.
type PasswordReset struct {
	Token     string
	UserID    int64
	ExpiresAt string
}

func (s *Store) GetEmailSettings() (EmailSettings, error) {
	var e EmailSettings
	var tls int
	err := s.db.QueryRow(`SELECT backend,host,port,username,password,from_addr,tls,api_base,api_key FROM email_settings WHERE id=1`).
		Scan(&e.Backend, &e.Host, &e.Port, &e.Username, &e.Password, &e.From, &tls, &e.APIBase, &e.APIKey)
	e.TLS = tls != 0
	return e, err
}

func (s *Store) SetEmailSettings(e EmailSettings) error {
	tls := 0
	if e.TLS {
		tls = 1
	}
	_, err := s.db.Exec(`UPDATE email_settings SET backend=?,host=?,port=?,username=?,password=?,from_addr=?,tls=?,api_base=?,api_key=? WHERE id=1`,
		e.Backend, e.Host, e.Port, e.Username, e.Password, e.From, tls, e.APIBase, e.APIKey)
	return err
}

func (s *Store) CreatePasswordReset(userID int64, token, expiresAt string) error {
	_, err := s.db.Exec(`INSERT INTO password_resets(token, user_id, expires_at) VALUES(?,?,?)`, token, userID, expiresAt)
	return err
}

func (s *Store) GetPasswordReset(token string) (*PasswordReset, bool) {
	var r PasswordReset
	err := s.db.QueryRow(`SELECT token, user_id, expires_at FROM password_resets WHERE token=?`, token).Scan(&r.Token, &r.UserID, &r.ExpiresAt)
	if err != nil {
		return nil, false
	}
	return &r, true
}

func (s *Store) DeletePasswordReset(token string) error {
	_, err := s.db.Exec(`DELETE FROM password_resets WHERE token=?`, token)
	return err
}

// ---- verifikasi email (pendaftaran mandiri hosted) — pola sama password reset ----

func (s *Store) CreateEmailVerification(userID int64, token, expiresAt string) error {
	_, err := s.db.Exec(`INSERT INTO email_verifications(token, user_id, expires_at) VALUES(?,?,?)`, token, userID, expiresAt)
	return err
}

// GetEmailVerification mengembalikan (userID, expiresAt, ok) untuk token.
func (s *Store) GetEmailVerification(token string) (int64, string, bool) {
	var uid int64
	var exp string
	err := s.db.QueryRow(`SELECT user_id, expires_at FROM email_verifications WHERE token=?`, token).Scan(&uid, &exp)
	if err != nil {
		return 0, "", false
	}
	return uid, exp, true
}

func (s *Store) DeleteEmailVerification(token string) error {
	_, err := s.db.Exec(`DELETE FROM email_verifications WHERE token=?`, token)
	return err
}

// GetUserByEmail mencari user berdasarkan alamat email (untuk alur forgot-password).
func (s *Store) GetUserByEmail(email string) (*User, error) {
	return scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE email=? AND email!=''`, email))
}
