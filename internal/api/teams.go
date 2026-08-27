package api

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lamun-my-id/lamund/internal/quota"
)

func (s *server) registerTeams(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/teams", s.requireAuth(s.handleListTeams))
	mux.HandleFunc("POST /api/v1/teams", s.requireAuth(s.handleCreateTeam))
	mux.HandleFunc("GET /api/v1/teams/{id}", s.requireAuth(s.handleGetTeam))
	mux.HandleFunc("DELETE /api/v1/teams/{id}", s.requireAuth(s.handleDeleteTeam))
	mux.HandleFunc("POST /api/v1/teams/{id}/members", s.requireAuth(s.handleAddMember))
	mux.HandleFunc("DELETE /api/v1/teams/{id}/members/{userId}", s.requireAuth(s.handleRemoveMember))
	mux.HandleFunc("POST /api/v1/teams/{id}/invite-link", s.requireAuth(s.handleCreateInviteLink))
	mux.HandleFunc("POST /api/v1/teams/{id}/invite-email", s.requireAuth(s.handleInviteEmail))
	mux.HandleFunc("POST /api/v1/teams/invites/{token}/accept", s.requireAuth(s.handleAcceptInvite))
	// Pencarian user untuk invite (ala GitHub): min 4 huruf, toleransi typo.
	mux.HandleFunc("GET /api/v1/users/search", s.requireAuth(s.handleSearchUsers))
}

// canAccessOwner adalah otorisasi inti R4: apakah user boleh melihat/mengubah
// resource dengan pemilik (ownerType, ownerID). Superadmin: selalu. Personal:
// hanya pemilik. Team: hanya anggota.
func (s *server) canAccessOwner(u *authUser, ownerType string, ownerID int64) bool {
	if u.Role == "superadmin" {
		return true
	}
	switch ownerType {
	case "team":
		_, ok := s.d.Store.GetMemberRole(ownerID, u.ID)
		return ok
	default: // "user" / kosong
		return ownerID == u.ID
	}
}

// canManageTeam: superadmin, atau owner/admin team tersebut.
func (s *server) canManageTeam(u *authUser, teamID int64) bool {
	if u.Role == "superadmin" {
		return true
	}
	role, ok := s.d.Store.GetMemberRole(teamID, u.ID)
	return ok && (role == "owner" || role == "admin")
}

type teamJSON struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Role        string `json:"role,omitempty"`
	MemberCount int    `json:"member_count"`
}

func (s *server) handleListTeams(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	out := []teamJSON{}
	if u.Role == "superadmin" {
		teams, err := s.d.Store.ListTeams()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "gagal membaca tim")
			return
		}
		for _, t := range teams {
			cnt, _ := s.d.Store.CountTeamMembers(t.ID)
			out = append(out, teamJSON{ID: t.ID, Name: t.Name, Slug: t.Slug, MemberCount: cnt})
		}
	} else {
		teams, err := s.d.Store.ListTeamsByUser(u.ID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "gagal membaca tim")
			return
		}
		for _, t := range teams {
			cnt, _ := s.d.Store.CountTeamMembers(t.ID)
			out = append(out, teamJSON{ID: t.ID, Name: t.Name, Slug: t.Slug, Role: t.Role, MemberCount: cnt})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"teams": out})
}

var slugRe = regexp.MustCompile(`[^a-z0-9-]+`)

func slugify(name string) string {
	s := slugRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	return strings.Trim(s, "-")
}

type createTeamReq struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (s *server) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	if !s.d.Store.CanCreateTeams(u.Role) {
		writeErr(w, http.StatusForbidden, "tidak boleh membuat tim")
		return
	}
	// Enforce kuota tim per-user (admin bisa setel; default DefaultMaxTeams).
	if okq, reason, err := quota.CanCreateTeam(s.d.Store, u.ID, u.Role); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal cek kuota")
		return
	} else if !okq {
		writeErr(w, http.StatusForbidden, reason)
		return
	}
	var req createTeamReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if len(req.Name) < 2 {
		writeErr(w, http.StatusBadRequest, "nama tim minimal 2 karakter")
		return
	}
	slug := slugify(req.Slug)
	if slug == "" {
		slug = slugify(req.Name)
	}
	if slug == "" {
		writeErr(w, http.StatusBadRequest, "slug tim tidak valid")
		return
	}
	t, err := s.d.Store.CreateTeam(req.Name, slug)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// Pembuat otomatis jadi owner.
	if err := s.d.Store.AddTeamMember(t.ID, u.ID, "owner"); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, teamJSON{ID: t.ID, Name: t.Name, Slug: t.Slug, Role: "owner"})
}

type memberJSON struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Role     string `json:"role"`
}

func (s *server) handleGetTeam(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	// Harus anggota atau superadmin.
	if _, member := s.d.Store.GetMemberRole(id, u.ID); !member && u.Role != "superadmin" {
		writeErr(w, http.StatusNotFound, "tim tidak ditemukan")
		return
	}
	t, err := s.d.Store.GetTeam(id)
	if err != nil || t == nil {
		writeErr(w, http.StatusNotFound, "tim tidak ditemukan")
		return
	}
	members, err := s.d.Store.ListTeamMembers(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca anggota")
		return
	}
	mj := make([]memberJSON, 0, len(members))
	for _, m := range members {
		mj = append(mj, memberJSON{m.UserID, m.Username, m.Name, m.Role})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": t.ID, "name": t.Name, "slug": t.Slug, "members": mj,
	})
}

type addMemberReq struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

func (s *server) handleAddMember(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if !s.canManageTeam(u, id) {
		writeErr(w, http.StatusForbidden, "butuh peran owner/admin tim")
		return
	}
	var req addMemberReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	target, err := s.d.Store.GetUserByUsername(strings.TrimSpace(req.Username))
	if err != nil || target == nil {
		writeErr(w, http.StatusNotFound, "pengguna tidak ditemukan")
		return
	}
	// Cegah eskalasi hak: hanya owner (atau superadmin) yang boleh memberi peran
	// 'owner' atau mengubah peran anggota yang saat ini owner. Team-admin tak boleh.
	callerRole, _ := s.d.Store.GetMemberRole(id, u.ID)
	privileged := u.Role == "superadmin" || callerRole == "owner"
	targetRole, targetIsMember := s.d.Store.GetMemberRole(id, target.ID)
	if !privileged && (req.Role == "owner" || (targetIsMember && targetRole == "owner")) {
		writeErr(w, http.StatusForbidden, "hanya owner tim yang bisa mengelola peran owner")
		return
	}
	if err := s.d.Store.AddTeamMember(id, target.ID, req.Role); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, memberJSON{target.ID, target.Username, target.Name, req.Role})
}

func (s *server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if !s.canManageTeam(u, id) {
		writeErr(w, http.StatusForbidden, "butuh peran owner/admin tim")
		return
	}
	targetID, err := parseInt64(r.PathValue("userId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "userId tidak valid")
		return
	}
	targetRole, isMember := s.d.Store.GetMemberRole(id, targetID)
	if !isMember {
		writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
		return
	}
	// Hanya owner/superadmin yang boleh mengeluarkan seorang owner…
	if targetRole == "owner" {
		callerRole, _ := s.d.Store.GetMemberRole(id, u.ID)
		if u.Role != "superadmin" && callerRole != "owner" {
			writeErr(w, http.StatusForbidden, "hanya owner tim yang bisa mengeluarkan owner")
			return
		}
		// …dan tim tak boleh ditinggalkan tanpa owner (yatim).
		if s.countTeamOwners(id) <= 1 {
			writeErr(w, http.StatusConflict, "tim harus punya minimal satu owner")
			return
		}
	}
	if err := s.d.Store.RemoveTeamMember(id, targetID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *server) countTeamOwners(teamID int64) int {
	members, err := s.d.Store.ListTeamMembers(teamID)
	if err != nil {
		return 0
	}
	n := 0
	for _, m := range members {
		if m.Role == "owner" {
			n++
		}
	}
	return n
}

func (s *server) handleDeleteTeam(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	// Hanya owner tim atau superadmin.
	role, member := s.d.Store.GetMemberRole(id, u.ID)
	if u.Role != "superadmin" && !(member && role == "owner") {
		writeErr(w, http.StatusForbidden, "hanya owner tim yang bisa menghapus")
		return
	}
	if err := s.d.Store.DeleteTeam(id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// resolveOwner memvalidasi permintaan owner (dari body create). Default personal.
// Bila owner_type=team, user harus anggota tim itu.
func (s *server) resolveOwner(u *authUser, ownerType string, ownerID int64) (string, int64, bool) {
	if ownerType == "team" && ownerID > 0 {
		if _, ok := s.d.Store.GetMemberRole(ownerID, u.ID); ok || u.Role == "superadmin" {
			return "team", ownerID, true
		}
		return "", 0, false
	}
	return "user", u.ID, true
}

func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

type inviteLinkReq struct {
	Role string `json:"role"`
}

// handleCreateInviteLink membuat link undangan tim (owner/admin tim atau superadmin).
// Token disimpan tanpa dihapus sehingga bisa dipakai berulang (multi-pakai).
func (s *server) handleCreateInviteLink(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if !s.canManageTeam(u, id) {
		writeErr(w, http.StatusForbidden, "butuh peran owner/admin tim")
		return
	}
	var req inviteLinkReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	if req.Role == "" {
		req.Role = "member"
	}
	if req.Role != "member" && req.Role != "admin" {
		writeErr(w, http.StatusBadRequest, "peran undangan harus member atau admin")
		return
	}
	token := randomHex(20)
	expiresAt := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
	if err := s.d.Store.CreateInvite(id, req.Role, token, expiresAt, ""); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"token": token,
		"path":  "/join/" + token,
	})
}

type inviteEmailReq struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// handleInviteEmail membuat invite token dan mengirimkan link /join/<token> via email.
func (s *server) handleInviteEmail(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if !s.canManageTeam(u, id) {
		writeErr(w, http.StatusForbidden, "butuh peran owner/admin tim")
		return
	}
	var req inviteEmailReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	if req.Role != "member" && req.Role != "admin" {
		writeErr(w, http.StatusBadRequest, "peran undangan harus member atau admin")
		return
	}
	if s.d.Mailer == nil {
		writeErr(w, http.StatusServiceUnavailable, "mailer belum dikonfigurasi")
		return
	}
	token := randomHex(20)
	expiresAt := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
	if err := s.d.Store.CreateInvite(id, req.Role, token, expiresAt, req.Email); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	body := "<p>Anda diundang bergabung ke tim Lamund.</p>" +
		"<p>Klik tautan berikut untuk bergabung: <a href=\"/join/" + token + "\">/join/" + token + "</a></p>"
	if err := s.d.Mailer.Send(req.Email, "Undangan tim Lamund", body); err != nil {
		writeErr(w, http.StatusBadGateway, "email tak terkirim — cek setelan email")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "terkirim"})
}

// handleAcceptInvite memproses penerimaan undangan: caller bergabung ke tim
// dengan peran yang ditentukan token. Token dihapus setelah berhasil (sekali-pakai).
func (s *server) handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	token := r.PathValue("token")
	inv, err := s.d.Store.GetInvite(token)
	if err != nil || inv == nil {
		writeErr(w, http.StatusNotFound, "undangan tidak ditemukan atau sudah kadaluarsa")
		return
	}
	// Periksa kadaluarsa.
	if inv.ExpiresAt != "" {
		exp, err := time.Parse(time.RFC3339, inv.ExpiresAt)
		if err != nil || time.Now().UTC().After(exp) {
			// Hapus token yang sudah kadaluarsa agar tidak menumpuk.
			_ = s.d.Store.DeleteInvite(token)
			writeErr(w, http.StatusGone, "undangan kadaluarsa")
			return
		}
	}
	// Periksa binding email bila undangan dikirim ke email tertentu.
	if inv.Email != "" && !strings.EqualFold(u.Email, inv.Email) {
		writeErr(w, http.StatusForbidden, "undangan ini untuk email lain")
		return
	}
	role := inv.Role
	if role != "member" && role != "admin" {
		role = "member"
	}
	if err := s.d.Store.AddTeamMember(inv.TeamID, u.ID, role); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Bakar token (sekali-pakai) setelah berhasil bergabung.
	_ = s.d.Store.DeleteInvite(token)
	writeJSON(w, http.StatusOK, map[string]any{
		"team_id": inv.TeamID,
		"role":    role,
	})
}
