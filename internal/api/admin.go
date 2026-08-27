package api

import (
	"net/http"
	"strconv"

	"github.com/lamun-my-id/lamund/internal/auth"
	"github.com/lamun-my-id/lamund/internal/store"
)

func (s *server) registerAdmin(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/users", s.requireAdmin(s.handleListUsers))
	mux.HandleFunc("POST /api/v1/users", s.requireAuth(s.handleCreateUser))
	mux.HandleFunc("DELETE /api/v1/users/{id}", s.requireAdmin(s.handleDeleteUser))
	mux.HandleFunc("PATCH /api/v1/users/{id}/quota", s.requireAdmin(s.handleSetQuota))
	mux.HandleFunc("PATCH /api/v1/users/{id}/status", s.requireAdmin(s.handleSetUserStatus))
	// Anti-lockout: superadmin bisa reset MFA user (hilang HP + recovery codes).
	mux.HandleFunc("POST /api/v1/admin/users/{id}/mfa/reset", s.requireAdmin(s.handleAdminMFAReset))

	// API keys: milik caller sendiri (bukan khusus admin).
	mux.HandleFunc("GET /api/v1/apikeys", s.requireAuth(s.handleListKeys))
	mux.HandleFunc("POST /api/v1/apikeys", s.requireAuth(s.handleCreateKey))
	mux.HandleFunc("DELETE /api/v1/apikeys/{id}", s.requireAuth(s.handleDeleteKey))
}

type userJSON struct {
	ID         int64  `json:"id"`
	Username   string `json:"username"`
	Role       string `json:"role"`
	Disabled   bool   `json:"disabled"`
	Email      string `json:"email"`
	MFAEnabled    bool   `json:"mfa_enabled"`
	MaxSites      int    `json:"max_sites"`
	MaxTeams      int    `json:"max_teams"`
	MaxMemoryMB   int    `json:"max_memory_mb"`
	MaxCPUPercent int    `json:"max_cpu_percent"`
	MaxApps       int    `json:"max_apps"`
	Approval      string `json:"approval"` // approved|pending|rejected
}

func (s *server) handleListUsers(w http.ResponseWriter, _ *http.Request) {
	users, err := s.d.Store.ListUsers()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca users")
		return
	}
	out := make([]userJSON, 0, len(users))
	for _, u := range users {
		_, mfaEnabled, _, _ := s.d.Store.GetMFA(u.ID)
		q, _ := s.d.Store.GetQuota(u.ID)
		maxSites, maxTeams, maxMem, maxCPU, maxApps := 0, 0, 0, 0, 0
		if q != nil {
			maxSites, maxTeams = q.MaxSites, q.MaxTeams
			maxMem, maxCPU, maxApps = q.MaxMemoryMB, q.MaxCPUPercent, q.MaxApps
		}
		out = append(out, userJSON{
			ID:         u.ID,
			Username:   u.Username,
			Role:       u.Role,
			Disabled:   u.Disabled,
			Email:      u.Email,
			MFAEnabled:    mfaEnabled,
			MaxSites:      maxSites,
			MaxTeams:      maxTeams,
			MaxMemoryMB:   maxMem,
			MaxCPUPercent: maxCPU,
			MaxApps:       maxApps,
			Approval:      u.Approval,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

// handleDeleteUser menghapus user setelah memvalidasi guard (self/last-superadmin/resource).
func (s *server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	caller, _ := userFrom(r.Context())
	if id == caller.ID {
		writeErr(w, http.StatusBadRequest, "tak bisa menghapus akun sendiri")
		return
	}
	target, err := s.d.Store.GetUserByID(id)
	if err != nil || target == nil {
		writeErr(w, http.StatusNotFound, "user tidak ditemukan")
		return
	}
	if target.Role == "superadmin" {
		n, err := s.d.Store.CountUsersByRole("superadmin")
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if n <= 1 {
			writeErr(w, http.StatusBadRequest, "tak bisa menghapus superadmin terakhir")
			return
		}
	}
	n, err := s.d.Store.CountOwnedResources(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n > 0 {
		writeErr(w, http.StatusConflict, "user masih punya situs/app/zona — pindahkan atau hapus dulu")
		return
	}
	if err := s.d.Store.DeleteUser(id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type createUserReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
	MaxSites int    `json:"max_sites"`
}

func (s *server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	if u.Role != "superadmin" && u.Role != "team_manager" {
		writeErr(w, http.StatusForbidden, "tak berhak membuat pengguna")
		return
	}
	var req createUserReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	if !validUsername(req.Username) || len(req.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "username 4-32 (huruf kecil/angka/-) & password min 8 karakter")
		return
	}
	// team_manager hanya boleh membuat member; superadmin bebas.
	if u.Role != "superadmin" {
		req.Role = "member"
	} else if req.Role != "superadmin" && req.Role != "team_manager" && req.Role != "member" {
		req.Role = "member"
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal hash password")
		return
	}
	id, err := s.d.Store.CreateUser(store.User{Username: req.Username, PasswordHash: hash, Role: req.Role})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.MaxSites > 0 {
		s.d.Store.SetQuota(store.Quota{UserID: id, MaxSites: req.MaxSites})
	}
	writeJSON(w, http.StatusCreated, userJSON{ID: id, Username: req.Username, Role: req.Role, Disabled: false})
}

type quotaReq struct {
	MaxSites       int `json:"max_sites"`
	MaxStorageMB   int `json:"max_storage_mb"`
	MaxBandwidthGB int `json:"max_bandwidth_gb"`
	MaxTeams       int `json:"max_teams"`
	MaxMemoryMB    int `json:"max_memory_mb"`
	MaxCPUPercent  int `json:"max_cpu_percent"`
	MaxApps        int `json:"max_apps"`
}

func (s *server) handleSetQuota(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if u, _ := s.d.Store.GetUserByID(id); u == nil {
		writeErr(w, http.StatusNotFound, "user tidak ditemukan")
		return
	}
	var req quotaReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	err := s.d.Store.SetQuota(store.Quota{
		UserID: id, MaxSites: req.MaxSites,
		MaxStorageMB: req.MaxStorageMB, MaxBandwidthGB: req.MaxBandwidthGB,
		MaxTeams: req.MaxTeams, MaxMemoryMB: req.MaxMemoryMB,
		MaxCPUPercent: req.MaxCPUPercent, MaxApps: req.MaxApps,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req)
}

type statusReq struct {
	Disabled bool `json:"disabled"`
}

func (s *server) handleSetUserStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req statusReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	if err := s.d.Store.SetUserDisabled(id, req.Disabled); err != nil {
		writeErr(w, http.StatusNotFound, "user tidak ditemukan")
		return
	}
	// Saat menonaktifkan user, batalkan semua token JWT lama yang beredar.
	if req.Disabled {
		_ = s.d.Store.BumpTokenVersion(id)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"disabled": req.Disabled})
}

// handleAdminMFAReset menonaktifkan MFA user lain (superadmin-only). Dipakai
// bila user kehilangan perangkat authenticator DAN recovery codes agar tidak
// terkunci selamanya. Route sudah dibungkus requireAdmin (superadmin-only).
func (s *server) handleAdminMFAReset(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if u, _ := s.d.Store.GetUserByID(id); u == nil {
		writeErr(w, http.StatusNotFound, "user tidak ditemukan")
		return
	}
	if err := s.d.Store.DisableMFA(id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "mfa_reset"})
}

// ---- API keys (caller's own) ----

func (s *server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	keys, err := s.d.Store.ListAPIKeys(u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca keys")
		return
	}
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]any{"id": k.ID, "name": k.Name, "last_used_at": k.LastUsedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"apikeys": out})
}

func (s *server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	var req struct {
		Name string `json:"name"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	plaintext, hash, err := auth.GenerateAPIKey()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal buat key")
		return
	}
	id, err := s.d.Store.CreateAPIKey(u.ID, req.Name, hash)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// key plaintext hanya ditampilkan SEKALI di sini.
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "name": req.Name, "key": plaintext})
}

func (s *server) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.d.Store.DeleteAPIKey(id, u.ID); err != nil {
		writeErr(w, http.StatusNotFound, "key tidak ditemukan")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id tidak valid")
		return 0, false
	}
	return id, true
}
