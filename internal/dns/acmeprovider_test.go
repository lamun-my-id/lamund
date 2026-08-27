package dns

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/libdns/libdns"
	"github.com/lamun-my-id/lamund/internal/store"
)

func TestACMEProvider_AppendDelete(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db")) // Open menjalankan migrasi
	if err != nil { t.Fatal(err) }
	defer st.Close()
	uid, _ := st.CreateUser(store.User{Username: "u", PasswordHash: "h", Role: "member"})
	if _, err := st.CreateDNSZoneWithSettings(store.DNSZone{Domain: "acme.com", UserID: uid}, store.DNSSettings{}); err != nil {
		t.Fatal(err)
	}
	reloaded := 0
	p := &ACMEProvider{Store: st, Reload: func() { reloaded++ }}
	rr := libdns.RR{Name: "_acme-challenge", Type: "TXT", Data: "tok-abc"}
	if _, err := p.AppendRecords(context.Background(), "acme.com.", []libdns.Record{rr}); err != nil {
		t.Fatalf("AppendRecords: %v", err)
	}
	// TXT harus ada
	z, _ := st.GetDNSZone("acme.com")
	recs, _ := st.ListDNSRecords(z.ID)
	found := false
	for _, r := range recs {
		if r.Type == "TXT" && r.Name == "_acme-challenge" && r.Value == "tok-abc" {
			found = true
		}
	}
	if !found { t.Fatal("TXT challenge tak dibuat") }
	if reloaded == 0 { t.Fatal("reload harus dipanggil setelah append") }

	if _, err := p.DeleteRecords(context.Background(), "acme.com.", []libdns.Record{rr}); err != nil {
		t.Fatalf("DeleteRecords: %v", err)
	}
	recs2, _ := st.ListDNSRecords(z.ID)
	for _, r := range recs2 {
		if r.Type == "TXT" { t.Fatal("TXT harus terhapus") }
	}

	// zona asing → error (fallback HTTP-01)
	if _, err := p.AppendRecords(context.Background(), "bukanmilik.org.", []libdns.Record{rr}); err == nil {
		t.Fatal("zona tak dikelola harus error")
	}
}
