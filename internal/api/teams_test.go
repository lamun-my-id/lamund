package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/lamun-my-id/lamund/internal/auth"
	"github.com/lamun-my-id/lamund/internal/store"
)

// newMember membuat user biasa + login, mengembalikan (id, token).
func newMember(t *testing.T, h http.Handler, st *store.Store, username string) (int64, string) {
	t.Helper()
	hash, _ := auth.HashPassword("rahasia123")
	id, err := st.CreateUser(store.User{Username: username, PasswordHash: hash, Role: "member"})
	if err != nil {
		t.Fatal(err)
	}
	rec := login(t, h, username, "rahasia123")
	if rec.Code != 200 {
		t.Fatalf("login %s gagal: %d", username, rec.Code)
	}
	var resp loginResp
	json.Unmarshal(rec.Body.Bytes(), &resp)
	return id, resp.Token
}

func siteDomains(t *testing.T, h http.Handler, token string) map[string]bool {
	t.Helper()
	rec := do(t, h, "GET", "/api/v1/sites", token, nil)
	if rec.Code != 200 {
		t.Fatalf("list sites: %d %s", rec.Code, rec.Body)
	}
	var resp struct {
		Sites []siteJSON `json:"sites"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	out := map[string]bool{}
	for _, s := range resp.Sites {
		out[s.Domain] = true
	}
	return out
}

// TestPersonalIsolation: dua user biasa tak saling melihat situs personal.
func TestPersonalIsolation(t *testing.T) {
	h, st := harness(t)
	idA, tokA := newMember(t, h, st, "alice")
	_, tokB := newMember(t, h, st, "bob")

	st.CreateSite(store.Site{Domain: "alice.test", Type: "static", UserID: idA, OwnerType: "user", OwnerID: idA})

	if !siteDomains(t, h, tokA)["alice.test"] {
		t.Fatal("alice harus lihat situs sendiri")
	}
	if siteDomains(t, h, tokB)["alice.test"] {
		t.Fatal("bob TIDAK boleh lihat situs personal alice")
	}
	// akses langsung juga 404 untuk bob
	if rec := do(t, h, "GET", "/api/v1/sites/alice.test", tokB, nil); rec.Code != 404 {
		t.Fatalf("bob akses situs alice harus 404, dapat %d", rec.Code)
	}
}

// TestTeamScoping: anggota Team A tak lihat resource Team B; anggota A lihat A.
func TestTeamScoping(t *testing.T) {
	h, st := harness(t)
	idA, tokA := newMember(t, h, st, "amember")
	idB, tokB := newMember(t, h, st, "bmember")

	teamA, _ := st.CreateTeam("Team A", "team-a")
	teamB, _ := st.CreateTeam("Team B", "team-b")
	st.AddTeamMember(teamA.ID, idA, "owner")
	st.AddTeamMember(teamB.ID, idB, "owner")

	st.CreateSite(store.Site{Domain: "a-team.test", Type: "static", UserID: idA, OwnerType: "team", OwnerID: teamA.ID})
	st.CreateSite(store.Site{Domain: "b-team.test", Type: "static", UserID: idB, OwnerType: "team", OwnerID: teamB.ID})

	da := siteDomains(t, h, tokA)
	if !da["a-team.test"] {
		t.Fatal("anggota A harus lihat resource Team A")
	}
	if da["b-team.test"] {
		t.Fatal("anggota A TIDAK boleh lihat resource Team B")
	}
	// akses langsung silang → 404
	if rec := do(t, h, "DELETE", "/api/v1/sites/b-team.test", tokA, nil); rec.Code != 404 {
		t.Fatalf("anggota A hapus situs Team B harus 404, dapat %d", rec.Code)
	}
	_ = tokB
}

// TestSuperadminSeesAll: role admin melihat semua resource lintas owner.
func TestSuperadminSeesAll(t *testing.T) {
	h, st := harness(t)
	idA, _ := newMember(t, h, st, "u1")
	team, _ := st.CreateTeam("T", "t")
	st.CreateSite(store.Site{Domain: "p.test", Type: "static", UserID: idA, OwnerType: "user", OwnerID: idA})
	st.CreateSite(store.Site{Domain: "t.test", Type: "static", UserID: idA, OwnerType: "team", OwnerID: team.ID})

	adminTok := loginToken(t, h) // admin dari harness
	d := siteDomains(t, h, adminTok)
	if !d["p.test"] || !d["t.test"] {
		t.Fatalf("superadmin harus lihat semua: %v", d)
	}
}

// TestCreateSiteTeamRequiresMembership: buat situs owner=team hanya jika anggota.
func TestCreateSiteTeamRequiresMembership(t *testing.T) {
	h, st := harness(t)
	idA, tokA := newMember(t, h, st, "carol")
	team, _ := st.CreateTeam("Secret", "secret") // carol BUKAN anggota

	body := map[string]any{"domain": "x.test", "type": "static", "owner_type": "team", "owner_id": team.ID}
	if rec := do(t, h, "POST", "/api/v1/sites", tokA, body); rec.Code != 403 {
		t.Fatalf("buat situs untuk tim yg bukan anggota harus 403, dapat %d: %s", rec.Code, rec.Body)
	}
	// setelah jadi anggota → boleh
	st.AddTeamMember(team.ID, idA, "member")
	if rec := do(t, h, "POST", "/api/v1/sites", tokA, body); rec.Code != 201 {
		t.Fatalf("anggota tim harus bisa buat situs tim, dapat %d: %s", rec.Code, rec.Body)
	}
	// situs itu owner=team
	site, _ := st.GetSiteByDomain("x.test")
	if site.OwnerType != "team" || site.OwnerID != team.ID {
		t.Fatalf("owner situs salah: %+v", site)
	}
}

// TestTeamMemberManagementAuthz: member tak boleh menambah anggota; owner boleh.
func TestTeamMemberManagementAuthz(t *testing.T) {
	h, st := harness(t)
	idOwner, tokOwner := newMember(t, h, st, "owner1")
	idMember, tokMember := newMember(t, h, st, "member1")
	newMember(t, h, st, "outsider")

	team, _ := st.CreateTeam("Crew", "crew")
	st.AddTeamMember(team.ID, idOwner, "owner")
	st.AddTeamMember(team.ID, idMember, "member")

	// member coba menambah outsider → 403
	add := map[string]string{"username": "outsider", "role": "member"}
	if rec := do(t, h, "POST", "/api/v1/teams/"+itoa(team.ID)+"/members", tokMember, add); rec.Code != 403 {
		t.Fatalf("member tambah anggota harus 403, dapat %d", rec.Code)
	}
	// owner menambah outsider → 200
	if rec := do(t, h, "POST", "/api/v1/teams/"+itoa(team.ID)+"/members", tokOwner, add); rec.Code != 200 {
		t.Fatalf("owner tambah anggota harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	_ = idMember
}

// TestAdminCannotEscalateToOwner: team-admin (bukan owner) tak boleh memberi
// peran owner, mengubah owner lain, atau mengeluarkan owner terakhir.
func TestAdminCannotEscalateToOwner(t *testing.T) {
	h, st := harness(t)
	idOwner, _ := newMember(t, h, st, "theowner")
	idAdmin, tokAdmin := newMember(t, h, st, "theadmin")
	newMember(t, h, st, "victim")

	team, _ := st.CreateTeam("Org", "org")
	st.AddTeamMember(team.ID, idOwner, "owner")
	st.AddTeamMember(team.ID, idAdmin, "admin")

	tid := itoa(team.ID)
	// admin memberi peran owner ke user lain → 403
	if rec := do(t, h, "POST", "/api/v1/teams/"+tid+"/members", tokAdmin,
		map[string]string{"username": "victim", "role": "owner"}); rec.Code != 403 {
		t.Fatalf("admin grant owner harus 403, dapat %d", rec.Code)
	}
	// admin mencoba mengubah peran owner yang ada → 403
	if rec := do(t, h, "POST", "/api/v1/teams/"+tid+"/members", tokAdmin,
		map[string]string{"username": "theowner", "role": "member"}); rec.Code != 403 {
		t.Fatalf("admin mengubah owner harus 403, dapat %d", rec.Code)
	}
	// admin mencoba mengeluarkan owner → 403
	if rec := do(t, h, "DELETE", "/api/v1/teams/"+tid+"/members/"+itoa(idOwner), tokAdmin, nil); rec.Code != 403 {
		t.Fatalf("admin mengeluarkan owner harus 403, dapat %d", rec.Code)
	}
}

// TestCannotRemoveLastOwner: owner tunggal tak bisa dihapus (tim yatim).
func TestCannotRemoveLastOwner(t *testing.T) {
	h, st := harness(t)
	idOwner, tokOwner := newMember(t, h, st, "solo")
	team, _ := st.CreateTeam("Solo", "solo")
	st.AddTeamMember(team.ID, idOwner, "owner")
	if rec := do(t, h, "DELETE", "/api/v1/teams/"+itoa(team.ID)+"/members/"+itoa(idOwner), tokOwner, nil); rec.Code != 409 {
		t.Fatalf("hapus owner terakhir harus 409, dapat %d", rec.Code)
	}
}

// TestMoveOwnerPersonalToTeam: pindah kepemilikan situs personal → tim.
func TestMoveOwnerPersonalToTeam(t *testing.T) {
	h, st := harness(t)
	idA, tokA := newMember(t, h, st, "dave")
	team, _ := st.CreateTeam("Dave Team", "dave-team")
	st.AddTeamMember(team.ID, idA, "owner")
	st.CreateSite(store.Site{Domain: "move.test", Type: "static", UserID: idA, OwnerType: "user", OwnerID: idA})

	// pindah ke tim
	if err := st.SetSiteOwner("move.test", "team", team.ID); err != nil {
		t.Fatal(err)
	}
	// masih terlihat oleh dave (anggota tim)
	if !siteDomains(t, h, tokA)["move.test"] {
		t.Fatal("setelah pindah ke tim, anggota tim harus tetap lihat")
	}
	site, _ := st.GetSiteByDomain("move.test")
	if site.OwnerType != "team" {
		t.Fatalf("owner_type harus team, dapat %s", site.OwnerType)
	}
}

func TestTeamMemberSeesOnlyOwnSites(t *testing.T) {
	h, st := harness(t)
	idOwner, _ := newMember(t, h, st, "towner")
	idMem, tokMem := newMember(t, h, st, "tmember")
	team, _ := st.CreateTeam("T", "t")
	st.AddTeamMember(team.ID, idOwner, "owner")
	st.AddTeamMember(team.ID, idMem, "member")
	// dua site milik tim, beda pembuat
	st.CreateSite(store.Site{Domain: "byowner.test", Type: "static", UserID: idOwner, OwnerType: "team", OwnerID: team.ID})
	st.CreateSite(store.Site{Domain: "bymem.test", Type: "static", UserID: idMem, OwnerType: "team", OwnerID: team.ID})
	d := siteDomains(t, h, tokMem)
	if !d["bymem.test"] || d["byowner.test"] {
		t.Fatalf("member harus lihat site buatannya saja: %v", d)
	}
	// owner lihat semua
	recO := login(t, h, "towner", "rahasia123")
	var ro loginResp
	json.Unmarshal(recO.Body.Bytes(), &ro)
	do2 := siteDomains(t, h, ro.Token)
	if !do2["byowner.test"] || !do2["bymem.test"] {
		t.Fatalf("owner harus lihat semua site tim: %v", do2)
	}
}

func TestInviteLinkFlow(t *testing.T) {
	h, st := harness(t)
	idOwner, tokOwner := newMember(t, h, st, "invowner")
	idJoiner, tokJoiner := newMember(t, h, st, "joiner")
	team, _ := st.CreateTeam("Crew", "crew")
	st.AddTeamMember(team.ID, idOwner, "owner")
	// owner bikin link
	rec := do(t, h, "POST", "/api/v1/teams/"+itoa(team.ID)+"/invite-link", tokOwner, map[string]string{"role": "member"})
	if rec.Code != 200 {
		t.Fatalf("buat link harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	var lk struct {
		Token string `json:"token"`
	}
	json.Unmarshal(rec.Body.Bytes(), &lk)
	if lk.Token == "" {
		t.Fatal("token kosong")
	}
	// joiner terima
	if rec := do(t, h, "POST", "/api/v1/teams/invites/"+lk.Token+"/accept", tokJoiner, nil); rec.Code != 200 {
		t.Fatalf("terima undangan harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	if role, ok := st.GetMemberRole(team.ID, idJoiner); !ok || role != "member" {
		t.Fatalf("joiner harus jadi member, ok=%v role=%s", ok, role)
	}
	_ = idJoiner
}

func TestInviteLinkRejectsOwnerRole(t *testing.T) {
	h, st := harness(t)
	idOwner, _ := newMember(t, h, st, "io2")
	idAdmin, tokAdmin := newMember(t, h, st, "ia2")
	team, _ := st.CreateTeam("Z", "zteam")
	st.AddTeamMember(team.ID, idOwner, "owner")
	st.AddTeamMember(team.ID, idAdmin, "admin")
	rec := do(t, h, "POST", "/api/v1/teams/"+itoa(team.ID)+"/invite-link", tokAdmin, map[string]string{"role": "owner"})
	if rec.Code != 400 {
		t.Fatalf("undangan role owner harus ditolak 400, dapat %d", rec.Code)
	}
	_ = idAdmin
}

func TestInviteEmailSends(t *testing.T) {
	cap := &capMailer{}
	h, st := harnessMail(t, cap)
	idOwner, tokOwner := newMember(t, h, st, "eowner")
	team, _ := st.CreateTeam("Mail", "mail")
	st.AddTeamMember(team.ID, idOwner, "owner")
	rec := do(t, h, "POST", "/api/v1/teams/"+itoa(team.ID)+"/invite-email", tokOwner, map[string]string{"email": "friend@x", "role": "member"})
	if rec.Code != 200 {
		t.Fatalf("invite-email 200, dapat %d: %s", rec.Code, rec.Body)
	}
	if cap.lastTo != "friend@x" || cap.joinToken == "" {
		t.Fatalf("email undangan tak terkirim benar: to=%s tok=%s", cap.lastTo, cap.joinToken)
	}
}

// TestAnyUserCanCreateTeam: setiap user login (termasuk role member) boleh membuat
// tim sendiri dan otomatis jadi owner. Bikin AKUN tetap admin-only; ini soal bikin TIM.
func TestAnyUserCanCreateTeam(t *testing.T) {
	h, st := harness(t) // admin=superadmin
	// member biasa boleh bikin tim → 201 + jadi owner
	hashM, _ := auth.HashPassword("rahasia123")
	st.CreateUser(store.User{Username: "plainmem", PasswordHash: hashM, Role: "member"})
	recM := login(t, h, "plainmem", "rahasia123")
	var rm loginResp
	json.Unmarshal(recM.Body.Bytes(), &rm)
	rec := do(t, h, "POST", "/api/v1/teams", rm.Token, map[string]string{"name": "Xteam"})
	if rec.Code != 201 {
		t.Fatalf("member buat tim harus 201, dapat %d: %s", rec.Code, rec.Body)
	}
	var tj teamJSON
	json.Unmarshal(rec.Body.Bytes(), &tj)
	if tj.Role != "owner" {
		t.Fatalf("pembuat tim harus jadi owner, dapat %q", tj.Role)
	}
}

// ---- Invite hardening (Task 3) ----

// newMemberWithEmail membuat user dengan email tertentu, mengembalikan (id, token).
func newMemberWithEmail(t *testing.T, h http.Handler, st *store.Store, username, email string) (int64, string) {
	t.Helper()
	hash, _ := auth.HashPassword("rahasia123")
	id, err := st.CreateUser(store.User{Username: username, PasswordHash: hash, Role: "member", Email: email})
	if err != nil {
		t.Fatal(err)
	}
	rec := login(t, h, username, "rahasia123")
	if rec.Code != 200 {
		t.Fatalf("login %s gagal: %d", username, rec.Code)
	}
	var resp loginResp
	json.Unmarshal(rec.Body.Bytes(), &resp)
	return id, resp.Token
}

// TestInviteHardeningFutureExpiry: invite dengan expiry masa depan → accept pertama
// berhasil dan token terhapus; accept kedua dengan token yang sama gagal 404.
func TestInviteHardeningFutureExpiry(t *testing.T) {
	h, st := harness(t)
	idOwner, tokOwner := newMember(t, h, st, "hard_owner")
	idJoiner, tokJoiner := newMember(t, h, st, "hard_joiner")
	team, _ := st.CreateTeam("HardCrew", "hardcrew")
	st.AddTeamMember(team.ID, idOwner, "owner")

	// Buat link invite via API (expiresAt = now+72h, email='')
	rec := do(t, h, "POST", "/api/v1/teams/"+itoa(team.ID)+"/invite-link", tokOwner, map[string]string{"role": "member"})
	if rec.Code != 200 {
		t.Fatalf("buat link harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	var lk struct {
		Token string `json:"token"`
	}
	json.Unmarshal(rec.Body.Bytes(), &lk)
	if lk.Token == "" {
		t.Fatal("token kosong")
	}

	// Accept pertama → berhasil.
	if rec := do(t, h, "POST", "/api/v1/teams/invites/"+lk.Token+"/accept", tokJoiner, nil); rec.Code != 200 {
		t.Fatalf("accept pertama harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	if role, ok := st.GetMemberRole(team.ID, idJoiner); !ok || role != "member" {
		t.Fatalf("joiner harus jadi member, ok=%v role=%s", ok, role)
	}

	// Accept kedua → token sudah terhapus (sekali-pakai) → 404.
	if rec := do(t, h, "POST", "/api/v1/teams/invites/"+lk.Token+"/accept", tokJoiner, nil); rec.Code != 404 {
		t.Fatalf("accept kedua dengan token sama harus 404 (sekali-pakai), dapat %d: %s", rec.Code, rec.Body)
	}
}

// TestInviteHardeningExpiredToken: invite dengan expiry masa lalu → accept ditolak.
func TestInviteHardeningExpiredToken(t *testing.T) {
	h, st := harness(t)
	idOwner, _ := newMember(t, h, st, "exp_owner")
	_, tokJoiner := newMember(t, h, st, "exp_joiner")
	team, _ := st.CreateTeam("ExpCrew", "expcrew")
	st.AddTeamMember(team.ID, idOwner, "owner")

	// Sisipkan invite langsung ke store dengan expiry 1 jam yang lalu.
	pastExpiry := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	if err := st.CreateInvite(team.ID, "member", "expiredtoken123", pastExpiry, ""); err != nil {
		t.Fatal(err)
	}

	// Accept → harus ditolak karena kadaluarsa (410 Gone atau 400).
	rec := do(t, h, "POST", "/api/v1/teams/invites/expiredtoken123/accept", tokJoiner, nil)
	if rec.Code != http.StatusGone && rec.Code != http.StatusBadRequest {
		t.Fatalf("accept undangan kadaluarsa harus 410 atau 400, dapat %d: %s", rec.Code, rec.Body)
	}

	// Token harus sudah dihapus oleh handler.
	inv, _ := st.GetInvite("expiredtoken123")
	if inv != nil {
		t.Fatal("token kadaluarsa harus dihapus setelah ditolak")
	}
}

// TestInviteHardeningEmailBinding: email-invite hanya bisa diterima oleh
// penerima yang emailnya cocok (case-insensitive); email lain → 403.
func TestInviteHardeningEmailBinding(t *testing.T) {
	cap := &capMailer{}
	h, st := harnessMail(t, cap)
	idOwner, tokOwner := newMember(t, h, st, "bind_owner")
	idA, tokA := newMemberWithEmail(t, h, st, "userA", "a@x.com")
	_, tokB := newMemberWithEmail(t, h, st, "userB", "b@x.com")
	team, _ := st.CreateTeam("BindCrew", "bindcrew")
	st.AddTeamMember(team.ID, idOwner, "owner")

	// Kirim invite via invite-email API ke "a@x.com".
	rec := do(t, h, "POST", "/api/v1/teams/"+itoa(team.ID)+"/invite-email", tokOwner,
		map[string]string{"email": "a@x.com", "role": "member"})
	if rec.Code != 200 {
		t.Fatalf("invite-email harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	tok := cap.joinToken
	if tok == "" {
		t.Fatal("token undangan kosong — email tak terkirim")
	}

	// Caller B (b@x.com) coba terima → 403.
	if rec := do(t, h, "POST", "/api/v1/teams/invites/"+tok+"/accept", tokB, nil); rec.Code != 403 {
		t.Fatalf("user email lain harus 403, dapat %d: %s", rec.Code, rec.Body)
	}

	// Caller A (a@x.com, case-insensitive "A@X.COM") → berhasil.
	// Update email user A ke uppercase untuk membuktikan case-insensitive.
	st.UpdateUserProfile(idA, "UserA", "A@X.COM")
	if rec := do(t, h, "POST", "/api/v1/teams/invites/"+tok+"/accept", tokA, nil); rec.Code != 200 {
		t.Fatalf("user a@x.com (case-insensitive) harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	if role, ok := st.GetMemberRole(team.ID, idA); !ok || role != "member" {
		t.Fatalf("userA harus jadi member, ok=%v role=%s", ok, role)
	}
}

// TestInviteHardeningRoleOwnerBlocked: undangan via API tidak boleh memberi
// role 'owner' — regresi untuk pembatasan yang sudah ada.
func TestInviteHardeningRoleOwnerBlocked(t *testing.T) {
	h, st := harness(t)
	idOwner, tokOwner := newMember(t, h, st, "rog_owner")
	team, _ := st.CreateTeam("RogCrew", "rogcrew")
	st.AddTeamMember(team.ID, idOwner, "owner")

	// Coba buat link invite dengan role=owner → harus 400.
	rec := do(t, h, "POST", "/api/v1/teams/"+itoa(team.ID)+"/invite-link", tokOwner,
		map[string]string{"role": "owner"})
	if rec.Code != 400 {
		t.Fatalf("link invite role owner harus ditolak 400, dapat %d: %s", rec.Code, rec.Body)
	}
}

// TestListTeamsMemberCount memastikan GET /teams mengembalikan member_count per tim.
func TestListTeamsMemberCount(t *testing.T) {
	h, st := harness(t)
	adminTok := loginToken(t, h)

	// Buat tim dengan 2 anggota
	idA, _ := newMember(t, h, st, "mc_alice")
	idB, _ := newMember(t, h, st, "mc_bob")
	team, _ := st.CreateTeam("CountCrew", "countcrew")
	st.AddTeamMember(team.ID, idA, "owner")
	st.AddTeamMember(team.ID, idB, "member")

	rec := do(t, h, "GET", "/api/v1/teams", adminTok, nil)
	if rec.Code != 200 {
		t.Fatalf("list teams harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	var resp struct {
		Teams []struct {
			ID          int64  `json:"id"`
			Name        string `json:"name"`
			MemberCount int    `json:"member_count"`
		} `json:"teams"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse resp: %v", err)
	}
	found := false
	for _, tm := range resp.Teams {
		if tm.Name == "CountCrew" {
			found = true
			if tm.MemberCount != 2 {
				t.Errorf("member_count harus 2, dapat %d", tm.MemberCount)
			}
		}
	}
	if !found {
		t.Fatal("tim CountCrew tidak ditemukan dalam respons")
	}
}
