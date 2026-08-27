package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lamun-my-id/lamund/internal/auth"
	"github.com/lamun-my-id/lamund/internal/store"
)

func TestDNSZonesAndRecords(t *testing.T) {
	h, st := harness(t)
	tokAdmin := loginToken(t, h)

	// superadmin set settings
	r := do(t, h, "PUT", "/api/v1/dns/settings", tokAdmin, map[string]any{
		"ns1": "ns1.lamund.my.id",
		"ns2": "ns2.lamund.my.id",
	})
	if r.Code != 200 {
		t.Fatalf("PUT settings: %d — %s", r.Code, r.Body)
	}

	// buat zona
	r = do(t, h, "POST", "/api/v1/dns/zones", tokAdmin, map[string]any{"domain": "example.com"})
	if r.Code != 201 {
		t.Fatalf("buat zona: %d — %s", r.Code, r.Body)
	}

	// tambah A record valid
	if c := do(t, h, "POST", "/api/v1/dns/zones/example.com/records", tokAdmin, map[string]any{
		"name": "www", "type": "A", "value": "1.2.3.4", "ttl": 300,
	}).Code; c != 201 {
		t.Fatalf("tambah A: %d", c)
	}

	// A invalid → 400
	if c := do(t, h, "POST", "/api/v1/dns/zones/example.com/records", tokAdmin, map[string]any{
		"name": "x", "type": "A", "value": "bukan-ip",
	}).Code; c != 400 {
		t.Fatalf("A invalid harus 400, dapat %d", c)
	}

	// CNAME di apex → 400
	if c := do(t, h, "POST", "/api/v1/dns/zones/example.com/records", tokAdmin, map[string]any{
		"name": "@", "type": "CNAME", "value": "target.com",
	}).Code; c != 400 {
		t.Fatalf("CNAME apex harus 400, dapat %d", c)
	}

	// lintas-tenant → 404
	_, tokOther := newMember(t, h, st, "dns-other-user")
	if c := do(t, h, "GET", "/api/v1/dns/zones/example.com", tokOther, nil).Code; c != 404 {
		t.Fatalf("lintas-tenant harus 404, dapat %d", c)
	}

	// 409 duplicate zone
	if c := do(t, h, "POST", "/api/v1/dns/zones", tokAdmin, map[string]any{
		"domain": "example.com",
	}).Code; c != 409 {
		t.Fatalf("duplikat zona harus 409, dapat %d", c)
	}

	// GET zona mengembalikan zone + records + nameservers + glue
	rec := do(t, h, "GET", "/api/v1/dns/zones/example.com", tokAdmin, nil)
	if rec.Code != 200 {
		t.Fatalf("GET zona harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	var zoneResp struct {
		Zone        map[string]any   `json:"zone"`
		Records     []map[string]any `json:"records"`
		Nameservers []string         `json:"nameservers"`
		Glue        []map[string]any `json:"glue"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &zoneResp); err != nil {
		t.Fatalf("unmarshal GET zona: %v", err)
	}
	if len(zoneResp.Nameservers) != 2 {
		t.Fatalf("harus ada 2 nameserver, dapat %d", len(zoneResp.Nameservers))
	}
	if len(zoneResp.Glue) != 2 {
		t.Fatalf("harus ada 2 glue record, dapat %d", len(zoneResp.Glue))
	}

	// List zones (superadmin lihat semua)
	listRec := do(t, h, "GET", "/api/v1/dns/zones", tokAdmin, nil)
	if listRec.Code != 200 {
		t.Fatalf("GET zones harus 200, dapat %d", listRec.Code)
	}
	var listResp struct {
		Zones []map[string]any `json:"zones"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal GET zones: %v", err)
	}
	if len(listResp.Zones) == 0 {
		t.Fatal("harus ada minimal 1 zona di list")
	}

	// Member baru tidak melihat zona admin
	_, tokMember := newMember(t, h, st, "dns-member-user")
	listRecMember := do(t, h, "GET", "/api/v1/dns/zones", tokMember, nil)
	if listRecMember.Code != 200 {
		t.Fatalf("GET zones member harus 200, dapat %d", listRecMember.Code)
	}
	var listRespMember struct {
		Zones []map[string]any `json:"zones"`
	}
	if err := json.Unmarshal(listRecMember.Body.Bytes(), &listRespMember); err != nil {
		t.Fatalf("unmarshal GET zones member: %v", err)
	}
	if len(listRespMember.Zones) != 0 {
		t.Fatalf("member tidak boleh melihat zona admin, dapat %d", len(listRespMember.Zones))
	}

	// Member tidak bisa akses settings
	if c := do(t, h, "GET", "/api/v1/dns/settings", tokMember, nil).Code; c != 403 {
		t.Fatalf("member GET settings harus 403, dapat %d", c)
	}
	if c := do(t, h, "PUT", "/api/v1/dns/settings", tokMember, map[string]any{"ns1": "x.com"}).Code; c != 403 {
		t.Fatalf("member PUT settings harus 403, dapat %d", c)
	}

	// DELETE zona
	if c := do(t, h, "DELETE", "/api/v1/dns/zones/example.com", tokAdmin, nil).Code; c != 200 {
		t.Fatalf("DELETE zona harus 200, dapat %d", c)
	}

	// Setelah dihapus, GET harus 404
	if c := do(t, h, "GET", "/api/v1/dns/zones/example.com", tokAdmin, nil).Code; c != 404 {
		t.Fatalf("GET setelah DELETE harus 404, dapat %d", c)
	}
}

func TestDNSRecordValidation(t *testing.T) {
	h, _ := harness(t)
	tok := loginToken(t, h)

	// Buat zona dulu
	if c := do(t, h, "POST", "/api/v1/dns/zones", tok, map[string]any{"domain": "valid.com"}).Code; c != 201 {
		t.Fatalf("buat zona: %d", c)
	}

	tests := []struct {
		name     string
		body     map[string]any
		wantCode int
	}{
		{"A valid", map[string]any{"name": "a", "type": "A", "value": "1.2.3.4"}, 201},
		{"A invalid", map[string]any{"name": "b", "type": "A", "value": "999.0.0.1"}, 400},
		{"A IPv6 sebagai A", map[string]any{"name": "c", "type": "A", "value": "2001:db8::1"}, 400},
		{"AAAA valid", map[string]any{"name": "d", "type": "AAAA", "value": "2001:db8::1"}, 201},
		{"AAAA invalid", map[string]any{"name": "e", "type": "AAAA", "value": "tidak-valid"}, 400},
		{"AAAA IPv4 sebagai AAAA", map[string]any{"name": "f", "type": "AAAA", "value": "1.2.3.4"}, 400},
		{"CNAME valid", map[string]any{"name": "cname", "type": "CNAME", "value": "target.example.com"}, 201},
		{"CNAME apex", map[string]any{"name": "@", "type": "CNAME", "value": "target.com"}, 400},
		{"NS valid", map[string]any{"name": "@", "type": "NS", "value": "ns3.example.com"}, 201},
		{"NS invalid", map[string]any{"name": "@", "type": "NS", "value": "tidak valid!"}, 400},
		{"MX valid", map[string]any{"name": "@", "type": "MX", "value": "mail.example.com", "priority": 10}, 201},
		{"MX negative priority", map[string]any{"name": "@", "type": "MX", "value": "mail.example.com", "priority": -1}, 400},
		{"TXT valid", map[string]any{"name": "@", "type": "TXT", "value": "v=spf1 include:spf.example.com ~all"}, 201},
		{"TXT CR/LF", map[string]any{"name": "@", "type": "TXT", "value": "abc\ndef"}, 400},
		{"CAA valid", map[string]any{"name": "@", "type": "CAA", "value": `0 issue "letsencrypt.org"`}, 201},
		{"CAA invalid", map[string]any{"name": "@", "type": "CAA", "value": "tidak valid"}, 400},
		{"unknown type", map[string]any{"name": "@", "type": "UNKNOWN", "value": "x"}, 400},
		{"nama invalid", map[string]any{"name": "invalid name!", "type": "A", "value": "1.2.3.4"}, 400},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := do(t, h, "POST", "/api/v1/dns/zones/valid.com/records", tok, tc.body).Code
			if c != tc.wantCode {
				t.Errorf("mau %d, dapat %d", tc.wantCode, c)
			}
		})
	}
}

func TestDNSNSApexProtection(t *testing.T) {
	h, _ := harness(t)
	tok := loginToken(t, h)

	// Buat zona tanpa NS bootstrap (settings kosong di harness default)
	if c := do(t, h, "POST", "/api/v1/dns/zones", tok, map[string]any{"domain": "nstest.com"}).Code; c != 201 {
		t.Fatalf("buat zona: %d", c)
	}

	// Tambah satu NS
	r := do(t, h, "POST", "/api/v1/dns/zones/nstest.com/records", tok, map[string]any{
		"name": "@", "type": "NS", "value": "ns1.example.com",
	})
	if r.Code != 201 {
		t.Fatalf("tambah NS: %d — %s", r.Code, r.Body)
	}

	// Ambil ID record NS dari respons
	var addResp struct {
		Records []struct {
			ID   float64 `json:"id"`
			Type string  `json:"type"`
			Name string  `json:"name"`
		} `json:"records"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &addResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	var nsID int64
	for _, rc := range addResp.Records {
		if rc.Type == "NS" && rc.Name == "@" {
			nsID = int64(rc.ID)
			break
		}
	}
	if nsID == 0 {
		t.Fatal("NS record tidak ditemukan di respons")
	}

	// Hapus NS satu-satunya harus ditolak
	nsPath := fmt.Sprintf("/api/v1/dns/zones/nstest.com/records/%d", nsID)
	if c := do(t, h, "DELETE", nsPath, tok, nil).Code; c != 400 {
		t.Fatalf("hapus NS terakhir harus 400, dapat %d", c)
	}

	// Tambah NS kedua, baru boleh hapus yang pertama
	do(t, h, "POST", "/api/v1/dns/zones/nstest.com/records", tok, map[string]any{
		"name": "@", "type": "NS", "value": "ns2.example.com",
	})
	if c := do(t, h, "DELETE", nsPath, tok, nil).Code; c != 200 {
		t.Fatalf("hapus NS bukan-terakhir harus 200, dapat %d", c)
	}
}

// TestDNSZoneForAndAutoProvision menguji endpoint zone-for dan helper autoProvisionDNS.
func TestDNSZoneForAndAutoProvision(t *testing.T) {
	h, st := harness(t)
	tok := loginToken(t, h)

	// Buat zona auto.com
	r := do(t, h, "POST", "/api/v1/dns/zones", tok, map[string]any{"domain": "auto.com"})
	if r.Code != 201 {
		t.Fatalf("buat zona auto.com: %d — %s", r.Code, r.Body)
	}

	// zone-for: subdomain tercakup → managed=true
	r = do(t, h, "GET", "/api/v1/dns/zone-for?domain=blog.auto.com", tok, nil)
	if r.Code != 200 {
		t.Fatalf("zone-for blog.auto.com: %d — %s", r.Code, r.Body)
	}
	var zf struct {
		Managed bool   `json:"managed"`
		Zone    string `json:"zone"`
		Label   string `json:"label"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &zf); err != nil {
		t.Fatalf("unmarshal zone-for: %v", err)
	}
	if !zf.Managed || zf.Zone != "auto.com" || zf.Label != "blog" {
		t.Fatalf("zone-for salah: %+v", zf)
	}

	// domain tak tercakup → managed=false
	r2 := do(t, h, "GET", "/api/v1/dns/zone-for?domain=x.lain.org", tok, nil)
	if r2.Code != 200 {
		t.Fatalf("zone-for x.lain.org: %d", r2.Code)
	}
	var zf2 struct {
		Managed bool `json:"managed"`
	}
	if err := json.Unmarshal(r2.Body.Bytes(), &zf2); err != nil {
		t.Fatalf("unmarshal zone-for2: %v", err)
	}
	if zf2.Managed {
		t.Fatal("domain asing harus managed=false")
	}

	// missing domain param → 400
	if c := do(t, h, "GET", "/api/v1/dns/zone-for", tok, nil).Code; c != 400 {
		t.Fatalf("zone-for tanpa domain harus 400, dapat %d", c)
	}

	// --- Unit test autoProvisionDNS langsung ---
	// Bangun server baru dengan PublicIP set ke "9.9.9.9" + store sama.
	secret, _ := auth.GenerateSecret()
	srv := &server{
		d: Deps{
			Store:    st,
			Issuer:   auth.NewIssuer(secret, time.Hour),
			PublicIP: "9.9.9.9",
		},
		limiter:      newLimiter(time.Now),
		deployStatus: map[string]string{},
		devPending:   map[int64]*devicePending{},
	}

	// Dapatkan user admin dari store.
	adminUser, err := st.GetUserByUsername("admin")
	if err != nil || adminUser == nil {
		t.Fatalf("gagal ambil user admin: %v", err)
	}
	u := &authUser{ID: adminUser.ID, Role: adminUser.Role}

	// Panggil autoProvisionDNS untuk www.auto.com.
	srv.autoProvisionDNS(u, "www.auto.com")

	// Verifikasi record www A 9.9.9.9 ada di zona auto.com.
	zone, _, ok := st.FindDNSZoneForDomain("www.auto.com")
	if !ok {
		t.Fatal("FindDNSZoneForDomain www.auto.com harus ok")
	}
	recs, err := st.ListDNSRecords(zone.ID)
	if err != nil {
		t.Fatalf("ListDNSRecords: %v", err)
	}
	found := false
	for _, rec := range recs {
		if rec.Name == "www" && rec.Type == "A" && rec.Value == "9.9.9.9" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("record www A 9.9.9.9 tidak ditemukan; records: %+v", recs)
	}

	// Idempoten: panggil lagi → record tidak digandakan.
	srv.autoProvisionDNS(u, "www.auto.com")
	recs2, _ := st.ListDNSRecords(zone.ID)
	count := 0
	for _, rec := range recs2 {
		if rec.Name == "www" && rec.Type == "A" && rec.Value == "9.9.9.9" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("record harus tepat 1 (idempoten), dapat %d", count)
	}

	// PublicIP kosong → tidak buat record.
	srv2 := &server{
		d: Deps{
			Store:    st,
			PublicIP: "",
		},
		deployStatus: map[string]string{},
		devPending:   map[int64]*devicePending{},
	}
	beforeCount := len(recs2)
	srv2.autoProvisionDNS(u, "other.auto.com")
	recs3, _ := st.ListDNSRecords(zone.ID)
	if len(recs3) != beforeCount {
		t.Fatalf("PublicIP kosong seharusnya tidak tambah record; sebelum=%d sesudah=%d", beforeCount, len(recs3))
	}

	_ = st
}

// harnessWithPublicIP membangun harness dengan PublicIP terisi.
// Dipakai bila tes butuh alur HTTP penuh + auto-provision.
func harnessWithPublicIP(t *testing.T, publicIP string) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hash, _ := auth.HashPassword("rahasia123")
	if _, err := st.CreateUser(store.User{Username: "admin", PasswordHash: hash, Role: "superadmin"}); err != nil {
		t.Fatal(err)
	}
	secret, _ := auth.GenerateSecret()
	h := New(Deps{Store: st, Issuer: auth.NewIssuer(secret, time.Hour), PublicIP: publicIP})
	return h, st
}

// TestDNSAutoCreateSiteHTTP menguji alur HTTP penuh: POST /sites {dns_auto:true}
// lalu GET /dns/zones/auto2.com/records harus memuat record A.
func TestDNSAutoCreateSiteHTTP(t *testing.T) {
	h, _ := harnessWithPublicIP(t, "9.9.9.9")

	// Login
	var resp loginResp
	rec := do(t, h, "POST", "/api/v1/auth/login", "", map[string]string{"username": "admin", "password": "rahasia123"})
	if rec.Code != 200 {
		t.Fatalf("login: %d", rec.Code)
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	tok := resp.Token

	// Buat zona
	if c := do(t, h, "POST", "/api/v1/dns/zones", tok, map[string]any{"domain": "auto2.com"}).Code; c != 201 {
		t.Fatalf("buat zona auto2.com: %d", c)
	}

	// POST /sites dengan dns_auto=true
	r := do(t, h, "POST", "/api/v1/sites", tok, map[string]any{
		"domain":   "www.auto2.com",
		"type":     "static",
		"dns_auto": true,
	})
	if r.Code != 201 {
		t.Fatalf("createSite: %d — %s", r.Code, r.Body)
	}

	// Verifikasi record melalui GET /dns/zones/auto2.com/records
	recResp := do(t, h, "GET", "/api/v1/dns/zones/auto2.com/records", tok, nil)
	if recResp.Code != 200 {
		t.Fatalf("GET records: %d", recResp.Code)
	}
	var recsJSON struct {
		Records []struct {
			Name  string `json:"name"`
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"records"`
	}
	if err := json.Unmarshal(recResp.Body.Bytes(), &recsJSON); err != nil {
		t.Fatalf("unmarshal records: %v", err)
	}
	found := false
	for _, rec := range recsJSON.Records {
		if rec.Name == "www" && rec.Type == "A" && rec.Value == "9.9.9.9" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("record www A 9.9.9.9 tidak ditemukan; records: %+v", recsJSON.Records)
	}
}

// TestDNSPatchValidationAndScope memastikan PATCH memvalidasi ulang nilai baru
// terhadap tipe tersimpan, dan mutasi record lintas-tenant ditolak (404).
func TestDNSPatchValidationAndScope(t *testing.T) {
	h, st := harness(t)
	tok := loginToken(t, h)

	if c := do(t, h, "POST", "/api/v1/dns/zones", tok, map[string]any{"domain": "patch.com"}).Code; c != 201 {
		t.Fatalf("buat zona: %d", c)
	}
	r := do(t, h, "POST", "/api/v1/dns/zones/patch.com/records", tok, map[string]any{
		"name": "www", "type": "A", "value": "1.2.3.4", "ttl": 300,
	})
	if r.Code != 201 {
		t.Fatalf("tambah A: %d — %s", r.Code, r.Body)
	}
	var resp struct {
		Records []struct {
			ID   float64 `json:"id"`
			Type string  `json:"type"`
		} `json:"records"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var id int64
	for _, rc := range resp.Records {
		if rc.Type == "A" {
			id = int64(rc.ID)
		}
	}
	if id == 0 {
		t.Fatal("A record id tak ditemukan")
	}
	path := fmt.Sprintf("/api/v1/dns/zones/patch.com/records/%d", id)

	// PATCH nilai invalid (bukan IPv4) untuk record tipe A → 400
	if c := do(t, h, "PATCH", path, tok, map[string]any{"value": "bukan-ip", "ttl": 60}).Code; c != 400 {
		t.Fatalf("PATCH A dgn nilai non-IP harus 400, dapat %d", c)
	}
	// PATCH nilai valid → 200
	if c := do(t, h, "PATCH", path, tok, map[string]any{"value": "5.6.7.8", "ttl": 60}).Code; c != 200 {
		t.Fatalf("PATCH A valid harus 200, dapat %d", c)
	}

	// Lintas-tenant: user lain PATCH/DELETE record zona ini → 404 (ownedZone)
	_, tokOther := newMember(t, h, st, "dns-patch-other")
	if c := do(t, h, "PATCH", path, tokOther, map[string]any{"value": "9.9.9.9"}).Code; c != 404 {
		t.Fatalf("PATCH lintas-tenant harus 404, dapat %d", c)
	}
	if c := do(t, h, "DELETE", path, tokOther, nil).Code; c != 404 {
		t.Fatalf("DELETE lintas-tenant harus 404, dapat %d", c)
	}
}
