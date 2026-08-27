package dns

import (
	"context"
	"fmt"
	"strings"

	"github.com/libdns/libdns"

	"github.com/lamun-my-id/lamund/internal/store"
)

// ACMEProvider mengimplementasikan libdns.RecordAppender dan libdns.RecordDeleter
// untuk zona yang dikelola Lamund. Dipakai oleh certmagic DNS01Solver agar
// Lamund dapat menerbitkan sertifikat wildcard via ACME DNS-01.
//
// Untuk zona yang TIDAK dikelola Lamund, kedua method mengembalikan error
// sehingga certmagic fallback ke HTTP-01.
type ACMEProvider struct {
	Store  *store.Store
	Reload func()
}

// Pastikan ACMEProvider memenuhi kedua interface libdns saat kompilasi.
var _ interface {
	libdns.RecordAppender
	libdns.RecordDeleter
} = (*ACMEProvider)(nil)

// AppendRecords menambahkan TXT record challenge ke zona yang dikelola Lamund.
// zone harus berupa FQDN dengan trailing dot (mis. "lamund.web.id.").
// Record non-TXT dilewati secara diam-diam.
// Setelah record ditambahkan, data plane di-reload agar NS segera menyajikannya.
func (p *ACMEProvider) AppendRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error) {
	apex := strings.TrimSuffix(zone, ".")
	z, _, ok := p.Store.FindDNSZoneForDomain(apex)
	if !ok || z.Domain != apex {
		return nil, fmt.Errorf("zona %q tidak dikelola lamund", apex)
	}

	anyAdded := false
	for _, rec := range recs {
		rr := rec.RR()
		if rr.Type != "TXT" {
			continue
		}
		added, err := p.Store.AddDNSRecordIfAbsent(store.DNSRecord{
			ZoneID: z.ID,
			Name:   rr.Name,
			Type:   "TXT",
			Value:  rr.Data,
			TTL:    60,
		})
		if err != nil {
			return nil, fmt.Errorf("tambah TXT %q ke zona %q: %w", rr.Name, apex, err)
		}
		if added {
			anyAdded = true
		}
	}

	if anyAdded && p.Reload != nil {
		p.Reload()
	}

	return recs, nil
}

// DeleteRecords menghapus TXT record challenge dari zona yang dikelola Lamund.
// zone harus berupa FQDN dengan trailing dot (mis. "lamund.web.id.").
// Record non-TXT dilewati secara diam-diam.
// Setelah record dihapus, data plane di-reload.
func (p *ACMEProvider) DeleteRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error) {
	apex := strings.TrimSuffix(zone, ".")
	z, _, ok := p.Store.FindDNSZoneForDomain(apex)
	if !ok || z.Domain != apex {
		return nil, fmt.Errorf("zona %q tidak dikelola lamund", apex)
	}

	anyDeleted := false
	for _, rec := range recs {
		rr := rec.RR()
		if rr.Type != "TXT" {
			continue
		}
		n, err := p.Store.DeleteDNSRecordsMatching(z.ID, rr.Name, "TXT", rr.Data)
		if err != nil {
			return nil, fmt.Errorf("hapus TXT %q dari zona %q: %w", rr.Name, apex, err)
		}
		if n > 0 {
			anyDeleted = true
		}
	}

	if anyDeleted && p.Reload != nil {
		p.Reload()
	}

	return recs, nil
}
