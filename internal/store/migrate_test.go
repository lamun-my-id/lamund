package store

import (
	"path/filepath"
	"sync"
	"testing"
)

// TestConcurrentMigration mereproduksi kondisi Batam: data plane & panel
// membuka DB belum-termigrasi bersamaan. Dengan BEGIN IMMEDIATE, semua
// harus berhasil (satu bermigrasi, sisanya menunggu lalu melewati).
func TestConcurrentMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "race.db")

	const n = 6
	var wg sync.WaitGroup
	errs := make([]error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start // buka serempak
			st, err := Open(path)
			if err != nil {
				errs[idx] = err
				return
			}
			defer st.Close()
			_, errs[idx] = st.CreateUser(User{Username: "u", PasswordHash: "h"})
		}(i)
	}
	close(start)
	wg.Wait()

	// Tepat satu goroutine berhasil membuat user "u" (UNIQUE); sisanya boleh
	// gagal karena duplikat — tapi TIDAK boleh gagal karena migrasi.
	migrateFailures := 0
	for _, err := range errs {
		if err != nil && err.Error() != "username u sudah dipakai" {
			migrateFailures++
			t.Logf("error tak terduga: %v", err)
		}
	}
	if migrateFailures > 0 {
		t.Fatalf("%d/%d proses gagal saat migrasi konkuren", migrateFailures, n)
	}
}
