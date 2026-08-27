package store

// mfa.go — metode MFA store: secret TOTP, status enabled, anti-replay lastStep,
// dan recovery codes (hashed, single-use). Semua query MFA TERPISAH dari
// scanUser/userCols agar path scan user tidak tersentuh.
//
// Secret MFA disimpan plaintext at-rest; enkripsi direncanakan batch-2.

// SetMFASecret menyimpan secret TOTP (enrollment awal; MFA belum diaktifkan).
func (s *Store) SetMFASecret(userID int64, secret string) error {
	res, err := s.db.Exec(`UPDATE users SET mfa_secret=? WHERE id=?`, secret, userID)
	return affectedID(res, err, userID)
}

// EnableMFA mengaktifkan MFA untuk user (dipanggil setelah verifikasi kode TOTP
// pertama saat enrollment berhasil).
func (s *Store) EnableMFA(userID int64) error {
	res, err := s.db.Exec(`UPDATE users SET mfa_enabled=1 WHERE id=?`, userID)
	return affectedID(res, err, userID)
}

// DisableMFA menonaktifkan MFA secara atomik: reset kolom MFA pada tabel users
// dan hapus semua recovery codes milik user dalam satu transaksi.
func (s *Store) DisableMFA(userID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE users SET mfa_enabled=0, mfa_secret='', mfa_last_step=0 WHERE id=?`,
		userID,
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(
		`DELETE FROM mfa_recovery_codes WHERE user_id=?`,
		userID,
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// GetMFA membaca status MFA user: secret TOTP, apakah diaktifkan, dan nilai
// mfa_last_step terakhir (dipakai anti-replay).
func (s *Store) GetMFA(userID int64) (secret string, enabled bool, lastStep int64, err error) {
	var enabledInt int
	err = s.db.QueryRow(
		`SELECT mfa_secret, mfa_enabled, mfa_last_step FROM users WHERE id=?`,
		userID,
	).Scan(&secret, &enabledInt, &lastStep)
	if err != nil {
		return "", false, 0, err
	}
	enabled = enabledInt != 0
	return secret, enabled, lastStep, nil
}

// SetMFALastStep menyimpan time-step TOTP yang terakhir berhasil diverifikasi.
// Dipanggil setiap kali verifikasi TOTP berhasil untuk mencegah replay
// (kode yang sama dalam window yang sama ditolak bila step <= lastStep).
func (s *Store) SetMFALastStep(userID, step int64) error {
	res, err := s.db.Exec(`UPDATE users SET mfa_last_step=? WHERE id=?`, step, userID)
	return affectedID(res, err, userID)
}

// AddRecoveryCodes menggantikan seluruh recovery codes milik user dengan set
// hash baru dalam satu transaksi (hapus lama → insert baru). Hash dihasilkan
// oleh caller (argon2 via auth.HashPassword).
func (s *Store) AddRecoveryCodes(userID int64, hashes []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM mfa_recovery_codes WHERE user_id=?`, userID); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, h := range hashes {
		if _, err := tx.Exec(
			`INSERT INTO mfa_recovery_codes(user_id, code_hash, used) VALUES(?,?,0)`,
			userID, h,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// ConsumeRecoveryCode mencari recovery code yang belum terpakai milik user,
// memanggil verify(hash) pada setiap baris. Bila verify mengembalikan true,
// kode tersebut ditandai used=1 dan fungsi mengembalikan (true, nil).
// Bila tidak ada yang cocok, mengembalikan (false, nil).
// verify biasanya melakukan perbandingan argon2 (lihat auth.VerifyPassword).
func (s *Store) ConsumeRecoveryCode(userID int64, verify func(hash string) bool) (bool, error) {
	rows, err := s.db.Query(
		`SELECT id, code_hash FROM mfa_recovery_codes WHERE user_id=? AND used=0`,
		userID,
	)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var hash string
		if err := rows.Scan(&id, &hash); err != nil {
			return false, err
		}
		if verify(hash) {
			// Tutup rows sebelum eksekusi UPDATE (hindari kunci berjenjang).
			rows.Close()
			if _, err := s.db.Exec(
				`UPDATE mfa_recovery_codes SET used=1 WHERE id=?`, id,
			); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}
