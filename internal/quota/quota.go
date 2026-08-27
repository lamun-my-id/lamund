// Package quota menegakkan batas per-user (jumlah situs, storage).
package quota

import (
	"fmt"

	"github.com/lamun-my-id/lamund/internal/store"
)

// Default dipakai bila user belum punya baris kuota eksplisit.
const (
	DefaultMaxSites     = 1
	DefaultMaxStorageMB = 100
	DefaultMaxTeams     = 3
	// Free-tier resource per app (admin bisa override mem/cpu per user).
	DefaultMaxMemoryMB   = 512 // RAM per app
	DefaultMaxCPUPercent = 50  // % dari 1 core (0.5 core)
	DefaultMaxPids       = 256 // batas proses/thread per app (anti fork-bomb; tetap)
	DefaultMaxApps       = 3   // jumlah app dimiliki
)

// AppLimits mengembalikan batas resource efektif untuk app milik userID:
// override quota bila di-set (>0), selain itu default free-tier. superadmin
// TAK dibatasi → (0,0,0) = tanpa cgroup limit.
func AppLimits(st *store.Store, userID int64) (memMB, cpuPct, pids int) {
	if u, _ := st.GetUserByID(userID); u != nil && u.Role == "superadmin" {
		return 0, 0, 0
	}
	memMB, cpuPct, pids = DefaultMaxMemoryMB, DefaultMaxCPUPercent, DefaultMaxPids
	if q, err := st.GetQuota(userID); err == nil && q != nil {
		if q.MaxMemoryMB > 0 {
			memMB = q.MaxMemoryMB
		}
		if q.MaxCPUPercent > 0 {
			cpuPct = q.MaxCPUPercent
		}
	}
	return memMB, cpuPct, pids
}

// CanCreateApp mengecek apakah user boleh membuat app lagi (kuota jumlah app).
func CanCreateApp(st *store.Store, userID int64, role string) (bool, string, error) {
	if role == "superadmin" {
		return true, "", nil
	}
	limit := DefaultMaxApps
	if q, err := st.GetQuota(userID); err != nil {
		return false, "", err
	} else if q != nil && q.MaxApps > 0 {
		limit = q.MaxApps
	}
	n, err := st.CountUserApps(userID)
	if err != nil {
		return false, "", err
	}
	if n >= limit {
		return false, fmt.Sprintf("kuota app tercapai (maks %d)", limit), nil
	}
	return true, "", nil
}

// CanCreateSite mengecek apakah user boleh membuat satu situs lagi.
// superadmin tak dibatasi. Mengembalikan (boleh, alasan, error).
func CanCreateSite(st *store.Store, userID int64, role string) (bool, string, error) {
	if role == "superadmin" {
		return true, "", nil
	}
	limit := DefaultMaxSites
	if q, err := st.GetQuota(userID); err != nil {
		return false, "", err
	} else if q != nil && q.MaxSites > 0 {
		limit = q.MaxSites
	}
	n, err := st.CountUserSites(userID)
	if err != nil {
		return false, "", err
	}
	if n >= limit {
		return false, fmt.Sprintf("kuota situs tercapai (maks %d)", limit), nil
	}
	return true, "", nil
}

// CanCreateTeam mengecek apakah user boleh MEMILIKI satu tim lagi (jadi owner).
// superadmin tak dibatasi. Batas = kuota per-user (bila >0) atau DefaultMaxTeams.
func CanCreateTeam(st *store.Store, userID int64, role string) (bool, string, error) {
	if role == "superadmin" {
		return true, "", nil
	}
	limit := DefaultMaxTeams
	if q, err := st.GetQuota(userID); err != nil {
		return false, "", err
	} else if q != nil && q.MaxTeams > 0 {
		limit = q.MaxTeams
	}
	n, err := st.CountTeamsOwnedByUser(userID)
	if err != nil {
		return false, "", err
	}
	if n >= limit {
		return false, fmt.Sprintf("kuota tim tercapai (maks %d)", limit), nil
	}
	return true, "", nil
}

// StorageLimitBytes mengembalikan batas storage user dalam byte.
// 0 = tanpa batas (superadmin, atau kuota tak disetel dan default 0 dimaknai bebas
// hanya untuk superadmin). Untuk non-superadmin memakai kuota atau default.
func StorageLimitBytes(st *store.Store, userID int64, role string) (int64, error) {
	if role == "superadmin" {
		return 0, nil
	}
	limitMB := DefaultMaxStorageMB
	if q, err := st.GetQuota(userID); err != nil {
		return 0, err
	} else if q != nil && q.MaxStorageMB > 0 {
		limitMB = q.MaxStorageMB
	}
	return int64(limitMB) * 1024 * 1024, nil
}

// CanUseStorage mengecek apakah tambahan addBytes masih dalam batas storage
// user (usedBytes = pemakaian sekarang). superadmin tak dibatasi.
func CanUseStorage(st *store.Store, userID int64, role string, usedBytes, addBytes int64) (bool, string, error) {
	if role == "superadmin" {
		return true, "", nil
	}
	limitMB := DefaultMaxStorageMB
	if q, err := st.GetQuota(userID); err != nil {
		return false, "", err
	} else if q != nil && q.MaxStorageMB > 0 {
		limitMB = q.MaxStorageMB
	}
	limitBytes := int64(limitMB) * 1024 * 1024
	if usedBytes+addBytes > limitBytes {
		return false, fmt.Sprintf("kuota storage tercapai (maks %d MB)", limitMB), nil
	}
	return true, "", nil
}
