package store

import "testing"

func setupDNS(t *testing.T) (*Store, int64) {
	t.Helper()
	st := openTemp(t)
	uid, err := st.CreateUser(User{Username: "u", PasswordHash: "h", Role: "member"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return st, uid
}

func TestCreateDNSZone_BootstrapNS(t *testing.T) {
	st, uid := setupDNS(t)
	if err := st.SetDNSSettings(DNSSettings{NS1: "ns1.lamund.my.id", NS2: "ns2.lamund.my.id"}); err != nil {
		t.Fatalf("SetDNSSettings: %v", err)
	}
	settings, _ := st.GetDNSSettings()
	zid, err := st.CreateDNSZoneWithSettings(DNSZone{Domain: "example.com", UserID: uid}, settings)
	if err != nil {
		t.Fatalf("CreateDNSZone: %v", err)
	}
	recs, err := st.ListDNSRecords(zid)
	if err != nil {
		t.Fatalf("ListDNSRecords: %v", err)
	}
	var ns int
	for _, r := range recs {
		if r.Type == "NS" && r.Name == "@" {
			ns++
		}
	}
	if ns != 2 {
		t.Fatalf("mau 2 NS apex, dapat %d (%v)", ns, recs)
	}
}

func TestAddDNSRecord_BumpsSerial(t *testing.T) {
	st, uid := setupDNS(t)
	zid, _ := st.CreateDNSZoneWithSettings(DNSZone{Domain: "example.com", UserID: uid}, DNSSettings{})
	z0, _ := st.GetDNSZone("example.com")
	if _, err := st.AddDNSRecord(DNSRecord{ZoneID: zid, Name: "www", Type: "A", Value: "1.2.3.4", TTL: 3600}); err != nil {
		t.Fatalf("AddDNSRecord: %v", err)
	}
	z1, _ := st.GetDNSZone("example.com")
	if z1.Serial <= z0.Serial {
		t.Fatalf("serial harus naik: %d → %d", z0.Serial, z1.Serial)
	}
}

func TestDeleteDNSZone_RemovesRecords(t *testing.T) {
	st, uid := setupDNS(t)
	zid, _ := st.CreateDNSZoneWithSettings(DNSZone{Domain: "example.com", UserID: uid}, DNSSettings{})
	must0(t, func() error { _, e := st.AddDNSRecord(DNSRecord{ZoneID: zid, Name: "a", Type: "A", Value: "1.1.1.1", TTL: 60}); return e }())
	if err := st.DeleteDNSZone("example.com"); err != nil {
		t.Fatalf("DeleteDNSZone: %v", err)
	}
	if z, _ := st.GetDNSZone("example.com"); z != nil {
		t.Fatal("zona harus terhapus")
	}
	// record anak harus ikut terhapus (FK cascade mati → cleanup eksplisit)
	if recs, _ := st.ListDNSRecords(zid); len(recs) != 0 {
		t.Fatalf("record anak harus ikut terhapus, masih ada %d", len(recs))
	}
	// domain bebas dipakai lagi
	if _, err := st.CreateDNSZoneWithSettings(DNSZone{Domain: "example.com", UserID: uid}, DNSSettings{}); err != nil {
		t.Fatalf("buat ulang zona harus sukses: %v", err)
	}
}

func TestFindDNSZoneForDomain(t *testing.T) {
	st, uid := setupDNS(t)
	if _, err := st.CreateDNSZoneWithSettings(DNSZone{Domain: "domainku.com", UserID: uid}, DNSSettings{}); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ in, wantZone, wantLabel string; wantOK bool }{
		{"blog.domainku.com", "domainku.com", "blog", true},
		{"a.b.domainku.com", "domainku.com", "a.b", true},
		{"domainku.com", "domainku.com", "@", true},
		{"notdomainku.com", "", "", false},       // batas label — bukan suffix sah
		{"lain.org", "", "", false},
	}
	for _, c := range cases {
		z, label, ok := st.FindDNSZoneForDomain(c.in)
		if ok != c.wantOK || (ok && (z.Domain != c.wantZone || label != c.wantLabel)) {
			t.Fatalf("%s → zone=%v label=%q ok=%v (mau %s/%q/%v)", c.in, z, label, ok, c.wantZone, c.wantLabel, c.wantOK)
		}
	}
}

func TestAddDNSRecordIfAbsent(t *testing.T) {
	st, uid := setupDNS(t)
	zid, _ := st.CreateDNSZoneWithSettings(DNSZone{Domain: "x.com", UserID: uid}, DNSSettings{})
	added, err := st.AddDNSRecordIfAbsent(DNSRecord{ZoneID: zid, Name: "www", Type: "A", Value: "1.2.3.4", TTL: 300})
	if err != nil || !added {
		t.Fatalf("insert pertama harus added: %v %v", added, err)
	}
	added2, err := st.AddDNSRecordIfAbsent(DNSRecord{ZoneID: zid, Name: "www", Type: "A", Value: "1.2.3.4", TTL: 300})
	if err != nil || added2 {
		t.Fatalf("insert kedua identik harus skip: %v %v", added2, err)
	}
}

func TestDeleteDNSRecordsMatching(t *testing.T) {
	st, uid := setupDNS(t)
	zid, _ := st.CreateDNSZoneWithSettings(DNSZone{Domain: "acme.com", UserID: uid}, DNSSettings{})
	must0(t, func() error { _, e := st.AddDNSRecordIfAbsent(DNSRecord{ZoneID: zid, Name: "_acme-challenge", Type: "TXT", Value: "tok123", TTL: 60}); return e }())
	z0, _ := st.GetDNSZone("acme.com")
	n, err := st.DeleteDNSRecordsMatching(zid, "_acme-challenge", "TXT", "tok123")
	if err != nil || n != 1 {
		t.Fatalf("hapus TXT: n=%d err=%v (mau 1)", n, err)
	}
	if recs, _ := st.ListDNSRecords(zid); len(recs) != 0 {
		t.Fatalf("TXT harus hilang, sisa %d", len(recs))
	}
	z1, _ := st.GetDNSZone("acme.com")
	if z1.Serial <= z0.Serial {
		t.Fatalf("serial harus naik setelah hapus: %d→%d", z0.Serial, z1.Serial)
	}
	// hapus yang tak ada → 0, no error
	n2, err := st.DeleteDNSRecordsMatching(zid, "nope", "TXT", "x")
	if err != nil || n2 != 0 {
		t.Fatalf("hapus tak-ada: n=%d err=%v (mau 0)", n2, err)
	}
}

func TestDNSSettings_RoundTrip(t *testing.T) {
	st, _ := setupDNS(t)
	if err := st.SetDNSSettings(DNSSettings{NS1: "a.example", NS2: "b.example", Hostmaster: "admin.example"}); err != nil {
		t.Fatalf("SetDNSSettings: %v", err)
	}
	got, _ := st.GetDNSSettings()
	if got.NS1 != "a.example" || got.NS2 != "b.example" || got.Hostmaster != "admin.example" {
		t.Fatalf("settings tidak round-trip: %+v", got)
	}
}
