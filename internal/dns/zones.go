// Package dns menyediakan authoritative DNS server in-memory untuk lamund.
// Tabel zona (Zones) di-atomic-swap seperti vhost.Table sehingga hot-reload
// tidak memerlukan restart proses. Server TIDAK pernah merekursi/forward —
// query zona yang tak di-host dikembalikan dengan REFUSED.
package dns

import (
	"net"
	"strconv"
	"strings"
	"sync/atomic"

	mdns "github.com/miekg/dns"

	"github.com/lamun-my-id/lamund/internal/store"
)

// Store adalah seam untuk test: implementasi nyata adalah *store.Store.
type Store interface {
	AllDNSData() ([]store.DNSZone, map[int64][]store.DNSRecord, store.DNSSettings, error)
}

// Snapshot adalah peta zona yang telah dibangun dari store, keyed oleh apex fqdn
// (huruf kecil, dengan trailing dot). Tak boleh dimutasi setelah dibuat.
type Snapshot map[string]*Zone

// Zone menyimpan satu zona DNS authoritative beserta index record in-memory.
type Zone struct {
	// Apex adalah nama domain tanpa trailing dot, huruf kecil.
	Apex string

	// Kolom SOA dari dns_zones.
	serial  uint32
	refresh uint32
	retry   uint32
	expire  uint32
	minimum uint32

	// Setelan NS dari dns_settings (per-instance).
	ns1        string
	ns2        string
	hostmaster string

	// records adalah index record: keyed ownerFQDN (huruf kecil, trailing dot).
	// "@" di store → apex fqdn; "*" → "*.apex."
	records map[string][]store.DNSRecord
}

// Zones adalah tabel zona in-memory yang dapat di-swap secara atomik.
type Zones struct {
	ptr atomic.Pointer[Snapshot]
}

// NewZones membuat Zones kosong.
func NewZones() *Zones {
	z := &Zones{}
	empty := make(Snapshot)
	z.ptr.Store(&empty)
	return z
}

// Swap mengganti snapshot secara atomik. Aman dipanggil dari goroutine manapun.
func (z *Zones) Swap(snap *Snapshot) {
	z.ptr.Store(snap)
}

// Find mencari zona yang merupakan suffix terpanjang dari qname.
// qname harus sudah dalam bentuk fqdn lowercase (trailing dot).
// Mengembalikan nil bila tidak ada zona yang cocok.
func (z *Zones) Find(qname string) *Zone {
	snap := *z.ptr.Load()
	// Iterasi dari label paling spesifik ke yang paling umum.
	// Contoh: "sub.example.com." → cek "sub.example.com.", "example.com.", "com.", "."
	name := qname
	for {
		if zone, ok := snap[name]; ok {
			return zone
		}
		// Lepas label terdepan.
		idx := strings.Index(name, ".")
		if idx < 0 || idx == len(name)-1 {
			break
		}
		name = name[idx+1:]
	}
	return nil
}

// BuildSnapshot membaca semua data DNS dari store dan membangun Snapshot baru.
func BuildSnapshot(st Store) (*Snapshot, error) {
	zones, recs, settings, err := st.AllDNSData()
	if err != nil {
		return nil, err
	}

	snap := make(Snapshot, len(zones))
	for _, z := range zones {
		apex := strings.ToLower(z.Domain)
		apexFQDN := mdns.Fqdn(apex)
		wildcardFQDN := "*." + apexFQDN

		zone := &Zone{
			Apex:       apex,
			serial:     uint32(z.Serial),
			refresh:    uint32(z.Refresh),
			retry:      uint32(z.Retry),
			expire:     uint32(z.Expire),
			minimum:    uint32(z.Minimum),
			ns1:        settings.NS1,
			ns2:        settings.NS2,
			hostmaster: settings.Hostmaster,
			records:    make(map[string][]store.DNSRecord),
		}

		for _, r := range recs[z.ID] {
			var ownerFQDN string
			switch r.Name {
			case "@":
				ownerFQDN = apexFQDN
			case "*":
				ownerFQDN = wildcardFQDN
			default:
				ownerFQDN = mdns.Fqdn(strings.ToLower(r.Name) + "." + apex)
			}
			zone.records[ownerFQDN] = append(zone.records[ownerFQDN], r)
		}

		snap[apexFQDN] = zone
	}

	return &snap, nil
}

// SOA membangun record SOA dari data zona dan setelan per-instance.
func (z *Zone) SOA() *mdns.SOA {
	apexFQDN := mdns.Fqdn(z.Apex)

	ns := z.ns1
	if ns == "" {
		ns = "."
	} else {
		ns = mdns.Fqdn(ns)
	}

	var mbox string
	if z.hostmaster != "" {
		// Pastikan trailing dot.
		mbox = mdns.Fqdn(z.hostmaster)
		// RFC 1035: hostmaster@example.com → hostmaster.example.com.
		// Bila sudah berformat email (mengandung @), konversi.
		if idx := strings.Index(mbox, "@"); idx >= 0 {
			mbox = strings.ReplaceAll(mbox[:idx], ".", "\\.") + "." + mbox[idx+1:]
		}
	} else {
		mbox = "hostmaster." + apexFQDN
	}

	return &mdns.SOA{
		Hdr: mdns.RR_Header{
			Name:   apexFQDN,
			Rrtype: mdns.TypeSOA,
			Class:  mdns.ClassINET,
			Ttl:    z.minimum,
		},
		Ns:      ns,
		Mbox:    mbox,
		Serial:  z.serial,
		Refresh: z.refresh,
		Retry:   z.retry,
		Expire:  z.expire,
		Minttl:  z.minimum,
	}
}

// NS mengembalikan semua record NS apex zona sebagai []dns.RR.
func (z *Zone) NS() []mdns.RR {
	apexFQDN := mdns.Fqdn(z.Apex)
	recs := z.records[apexFQDN]
	var out []mdns.RR
	for _, r := range recs {
		if r.Type != "NS" {
			continue
		}
		out = append(out, &mdns.NS{
			Hdr: mdns.RR_Header{
				Name:   apexFQDN,
				Rrtype: mdns.TypeNS,
				Class:  mdns.ClassINET,
				Ttl:    uint32(r.TTL),
			},
			Ns: mdns.Fqdn(r.Value),
		})
	}
	return out
}

// Match mencari record yang cocok dengan ownerFQDN dan qtype.
// Bila tidak ada exact match, mencoba wildcard *.apex.
// Mengembalikan:
//   - answers: RR yang cocok (kosong bila tidak ada)
//   - matchedName: true bila nama ini dikenal (ada record apa pun), beda NODATA vs NXDOMAIN
func (z *Zone) Match(ownerFQDN string, qtype uint16) (answers []mdns.RR, matchedName bool) {
	// Coba exact match terlebih dahulu.
	recs, exactExists := z.records[ownerFQDN]
	if exactExists {
		matchedName = true
		answers = filterByType(ownerFQDN, recs, qtype)
		if len(answers) > 0 {
			return answers, true
		}
		// Nama dikenal tapi type tidak ada (NODATA).
		return nil, true
	}

	// Coba wildcard: *.apex.
	wildcardFQDN := "*." + mdns.Fqdn(z.Apex)
	wrecs, wildcardExists := z.records[wildcardFQDN]
	if wildcardExists {
		// Wildcard match — gunakan ownerFQDN asli sebagai nama di header RR.
		answers = filterByType(ownerFQDN, wrecs, qtype)
		if len(answers) > 0 {
			return answers, true
		}
		// Wildcard ada tapi type tidak cocok → NODATA.
		return nil, true
	}

	// Nama tidak dikenal sama sekali → NXDOMAIN.
	return nil, false
}

// filterByType mengonversi slice DNSRecord ke []dns.RR sesuai qtype.
// ownerFQDN dipakai sebagai Name pada RR header.
func filterByType(ownerFQDN string, recs []store.DNSRecord, qtype uint16) []mdns.RR {
	var out []mdns.RR
	for _, r := range recs {
		rr := toRR(ownerFQDN, r)
		if rr == nil {
			continue
		}
		if qtype == mdns.TypeANY || rr.Header().Rrtype == qtype {
			out = append(out, rr)
		}
	}
	return out
}

// toRR mengonversi store.DNSRecord ke dns.RR dengan membangun struct secara langsung
// (BUKAN dns.NewRR(fmt.Sprintf(...))) untuk menghindari risiko injeksi/parse-fragility.
func toRR(ownerFQDN string, r store.DNSRecord) mdns.RR {
	ttl := uint32(r.TTL)
	hdr := func(rrtype uint16) mdns.RR_Header {
		return mdns.RR_Header{
			Name:   ownerFQDN,
			Rrtype: rrtype,
			Class:  mdns.ClassINET,
			Ttl:    ttl,
		}
	}

	switch r.Type {
	case "A":
		ip := net.ParseIP(r.Value)
		if ip == nil {
			return nil
		}
		ip4 := ip.To4()
		if ip4 == nil {
			return nil
		}
		return &mdns.A{Hdr: hdr(mdns.TypeA), A: ip4}

	case "AAAA":
		ip := net.ParseIP(r.Value)
		if ip == nil {
			return nil
		}
		ip6 := ip.To16()
		if ip6 == nil || ip.To4() != nil {
			return nil
		}
		return &mdns.AAAA{Hdr: hdr(mdns.TypeAAAA), AAAA: ip6}

	case "CNAME":
		return &mdns.CNAME{Hdr: hdr(mdns.TypeCNAME), Target: mdns.Fqdn(r.Value)}

	case "MX":
		return &mdns.MX{
			Hdr:        hdr(mdns.TypeMX),
			Preference: uint16(r.Priority),
			Mx:         mdns.Fqdn(r.Value),
		}

	case "TXT":
		// Satu character-string DNS maksimal 255 byte; pecah nilai panjang
		// (DKIM/SPF sering >255) menjadi beberapa segmen agar m.Pack() tak gagal.
		return &mdns.TXT{Hdr: hdr(mdns.TypeTXT), Txt: chunkTXT(r.Value)}

	case "NS":
		return &mdns.NS{Hdr: hdr(mdns.TypeNS), Ns: mdns.Fqdn(r.Value)}

	case "CAA":
		// Value format: "<flags> <tag> <value>" mis. "0 issue letsencrypt.org"
		parts := strings.SplitN(r.Value, " ", 3)
		if len(parts) != 3 {
			return nil
		}
		n, err := strconv.Atoi(parts[0])
		if err != nil || n < 0 || n > 255 {
			return nil // flags CAA hanya 0-255 (uint8); nilai lain diabaikan
		}
		return &mdns.CAA{
			Hdr:   hdr(mdns.TypeCAA),
			Flag:  uint8(n),
			Tag:   parts[1],
			Value: parts[2],
		}
	}

	return nil
}

// chunkTXT memecah string TXT menjadi segmen ≤255 byte (batas character-string
// DNS). String kosong tetap menghasilkan satu segmen kosong yang valid.
func chunkTXT(s string) []string {
	const max = 255
	if len(s) <= max {
		return []string{s}
	}
	var out []string
	for len(s) > max {
		out = append(out, s[:max])
		s = s[max:]
	}
	return append(out, s)
}
