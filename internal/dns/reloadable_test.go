package dns

import (
	"errors"
	"testing"

	mdns "github.com/miekg/dns"
	"github.com/lamun-my-id/lamund/internal/store"
)

// mutStore adalah fakeStore yang field-nya bisa diubah setelah dibuat.
type mutStore struct {
	zones []store.DNSZone
	recs  map[int64][]store.DNSRecord
	set   store.DNSSettings
	err   error // bila non-nil, AllDNSData mengembalikan error ini
}

func (m *mutStore) AllDNSData() ([]store.DNSZone, map[int64][]store.DNSRecord, store.DNSSettings, error) {
	if m.err != nil {
		return nil, nil, store.DNSSettings{}, m.err
	}
	return m.zones, m.recs, m.set, nil
}

func TestReloadableZones_Reload(t *testing.T) {
	fs := &mutStore{set: store.DNSSettings{NS1: "ns1.x", NS2: "ns2.x"}}
	fs.zones = []store.DNSZone{{ID: 1, Domain: "a.com", Serial: 1, Minimum: 3600}}
	fs.recs = map[int64][]store.DNSRecord{1: {{ZoneID: 1, Name: "@", Type: "A", Value: "1.1.1.1", TTL: 60}}}

	z, err := NewReloadableZones(fs)
	if err != nil {
		t.Fatal(err)
	}
	if z.Find(mdns.Fqdn("a.com")) == nil {
		t.Fatal("a.com harus ada setelah NewReloadableZones")
	}

	// Tambah zona kedua dan reload.
	fs.zones = append(fs.zones, store.DNSZone{ID: 2, Domain: "b.com", Serial: 1, Minimum: 3600})
	fs.recs[2] = []store.DNSRecord{{ZoneID: 2, Name: "@", Type: "A", Value: "2.2.2.2", TTL: 60}}
	if err := z.Reload(fs); err != nil {
		t.Fatal(err)
	}
	if z.Find(mdns.Fqdn("b.com")) == nil {
		t.Fatal("b.com harus muncul setelah reload")
	}
	// Zona lama tetap ada.
	if z.Find(mdns.Fqdn("a.com")) == nil {
		t.Fatal("a.com harus tetap ada setelah reload")
	}
}

func TestReloadableZones_ReloadError_KeepsOldSnapshot(t *testing.T) {
	fs := &mutStore{set: store.DNSSettings{NS1: "ns1.x"}}
	fs.zones = []store.DNSZone{{ID: 1, Domain: "a.com", Serial: 1, Minimum: 3600}}
	fs.recs = map[int64][]store.DNSRecord{1: {{ZoneID: 1, Name: "@", Type: "A", Value: "1.1.1.1", TTL: 60}}}

	z, err := NewReloadableZones(fs)
	if err != nil {
		t.Fatal(err)
	}

	// Buat store mengembalikan error.
	fs.err = errors.New("db down")
	if err := z.Reload(fs); err == nil {
		t.Fatal("Reload harus return error saat AllDNSData gagal")
	}

	// Snapshot lama harus tetap melayani — a.com masih harus ada.
	if z.Find(mdns.Fqdn("a.com")) == nil {
		t.Fatal("a.com harus tetap ada setelah reload gagal (snapshot lama dipertahankan)")
	}
}
