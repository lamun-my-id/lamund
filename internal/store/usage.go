package store

import (
	"database/sql"
	"errors"
)

// Usage adalah pemakaian bulanan seorang user.
type Usage struct {
	UserID         int64
	YYYYMM         string
	BandwidthBytes int64
	StorageBytes   int64
}

// AddBandwidth menambah pemakaian bandwidth user untuk bulan yyyymm (mis. "202608").
func (s *Store) AddBandwidth(userID int64, yyyymm string, bytes int64) error {
	_, err := s.db.Exec(
		`INSERT INTO usage_monthly(user_id, yyyymm, bandwidth_bytes) VALUES(?,?,?)
		 ON CONFLICT(user_id, yyyymm) DO UPDATE SET bandwidth_bytes = bandwidth_bytes + excluded.bandwidth_bytes`,
		userID, yyyymm, bytes)
	return err
}

// SetStorageBytes menetapkan snapshot pemakaian storage user untuk bulan yyyymm.
func (s *Store) SetStorageBytes(userID int64, yyyymm string, bytes int64) error {
	_, err := s.db.Exec(
		`INSERT INTO usage_monthly(user_id, yyyymm, storage_bytes) VALUES(?,?,?)
		 ON CONFLICT(user_id, yyyymm) DO UPDATE SET storage_bytes = excluded.storage_bytes`,
		userID, yyyymm, bytes)
	return err
}

// GetUsage mengembalikan pemakaian user untuk bulan yyyymm (nol bila belum ada).
func (s *Store) GetUsage(userID int64, yyyymm string) (Usage, error) {
	u := Usage{UserID: userID, YYYYMM: yyyymm}
	err := s.db.QueryRow(
		`SELECT bandwidth_bytes, storage_bytes FROM usage_monthly WHERE user_id=? AND yyyymm=?`,
		userID, yyyymm).Scan(&u.BandwidthBytes, &u.StorageBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return u, nil
	}
	return u, err
}
