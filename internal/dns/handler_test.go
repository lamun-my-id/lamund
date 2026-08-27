package dns

import (
	"net"
	"strings"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/lamun-my-id/lamund/internal/store"
)

// fakeStore memenuhi seam Store.
type fakeStore struct {
	zones []store.DNSZone
	recs  map[int64][]store.DNSRecord
	set   store.DNSSettings
}

func (f fakeStore) AllDNSData() ([]store.DNSZone, map[int64][]store.DNSRecord, store.DNSSettings, error) {
	return f.zones, f.recs, f.set, nil
}

// capWriter menangkap pesan balasan.
// addr dapat diset untuk mensimulasikan RemoteAddr yang berbeda-beda.
type capWriter struct {
	mdns.ResponseWriter
	msg  *mdns.Msg
	addr net.Addr
}

func (c *capWriter) WriteMsg(m *mdns.Msg) error { c.msg = m; return nil }

// RemoteAddr mengembalikan addr yang disimulasikan (atau addr default).
func (c *capWriter) RemoteAddr() net.Addr {
	if c.addr != nil {
		return c.addr
	}
	// Alamat default agar test lama tetap jalan.
	return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5353}
}

// fakeAddr adalah net.Addr sederhana untuk test.
type fakeAddr struct{ s string }

func (f fakeAddr) Network() string { return "udp" }
func (f fakeAddr) String() string  { return f.s }

func buildHandler(t *testing.T) *Handler {
	t.Helper()
	fs := fakeStore{
		zones: []store.DNSZone{{ID: 1, Domain: "example.com", Serial: 5, Refresh: 7200, Retry: 3600, Expire: 1209600, Minimum: 3600}},
		recs: map[int64][]store.DNSRecord{1: {
			{ZoneID: 1, Name: "@", Type: "NS", Value: "ns1.lamund.my.id", TTL: 3600},
			{ZoneID: 1, Name: "@", Type: "NS", Value: "ns2.lamund.my.id", TTL: 3600},
			{ZoneID: 1, Name: "@", Type: "A", Value: "9.9.9.9", TTL: 300},
			{ZoneID: 1, Name: "www", Type: "A", Value: "1.2.3.4", TTL: 300},
			{ZoneID: 1, Name: "www", Type: "AAAA", Value: "2001:db8::1", TTL: 300},
			{ZoneID: 1, Name: "*", Type: "A", Value: "8.8.8.8", TTL: 300},
		}},
		set: store.DNSSettings{NS1: "ns1.lamund.my.id", NS2: "ns2.lamund.my.id"},
	}
	snap, err := BuildSnapshot(fs)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	z := NewZones()
	z.Swap(snap)
	return NewHandler(z)
}

func query(t *testing.T, h *Handler, name string, qtype uint16) *mdns.Msg {
	t.Helper()
	req := new(mdns.Msg)
	req.SetQuestion(mdns.Fqdn(name), qtype)
	w := &capWriter{}
	h.ServeDNS(w, req)
	if w.msg == nil {
		t.Fatal("tak ada balasan")
	}
	return w.msg
}

func TestServeDNS_ARecord(t *testing.T) {
	h := buildHandler(t)
	m := query(t, h, "www.example.com", mdns.TypeA)
	if m.Rcode != mdns.RcodeSuccess || !m.Authoritative {
		t.Fatalf("mau NOERROR+AA, dapat rcode=%d aa=%v", m.Rcode, m.Authoritative)
	}
	if len(m.Answer) != 1 {
		t.Fatalf("mau 1 answer, dapat %d", len(m.Answer))
	}
	if a, ok := m.Answer[0].(*mdns.A); !ok || a.A.String() != "1.2.3.4" {
		t.Fatalf("A record salah: %v", m.Answer[0])
	}
	if m.RecursionAvailable {
		t.Fatal("RA harus false (bukan resolver)")
	}
}

func TestServeDNS_Refused_ForeignZone(t *testing.T) {
	h := buildHandler(t)
	m := query(t, h, "www.tidakada.org", mdns.TypeA)
	if m.Rcode != mdns.RcodeRefused {
		t.Fatalf("zona asing harus REFUSED, dapat %d", m.Rcode)
	}
}

func TestServeDNS_NXDOMAIN_WithSOA(t *testing.T) {
	h := buildHandler(t)
	// nama tak ada, TAPI wildcard '*' ada → RFC: wildcard match, jadi pakai nama yg pasti tak match wildcard? '*' match semua 1-label.
	// Uji NODATA: www ada, minta MX → NOERROR kosong + SOA di authority.
	m := query(t, h, "www.example.com", mdns.TypeMX)
	if m.Rcode != mdns.RcodeSuccess || len(m.Answer) != 0 {
		t.Fatalf("NODATA: mau NOERROR kosong, dapat rcode=%d answers=%d", m.Rcode, len(m.Answer))
	}
	if len(m.Ns) != 1 {
		t.Fatalf("NODATA harus punya SOA di authority, dapat %d", len(m.Ns))
	}
	if _, ok := m.Ns[0].(*mdns.SOA); !ok {
		t.Fatalf("authority harus SOA, dapat %T", m.Ns[0])
	}
}

func TestServeDNS_ApexSOAandNS(t *testing.T) {
	h := buildHandler(t)
	soa := query(t, h, "example.com", mdns.TypeSOA)
	if len(soa.Answer) != 1 {
		t.Fatalf("apex SOA: mau 1 answer, dapat %d", len(soa.Answer))
	}
	if s, ok := soa.Answer[0].(*mdns.SOA); !ok || s.Serial != 5 {
		t.Fatalf("SOA salah: %v", soa.Answer[0])
	}
	ns := query(t, h, "example.com", mdns.TypeNS)
	if len(ns.Answer) != 2 {
		t.Fatalf("apex NS: mau 2, dapat %d", len(ns.Answer))
	}
}

func TestServeDNS_Wildcard(t *testing.T) {
	h := buildHandler(t)
	m := query(t, h, "apapun.example.com", mdns.TypeA)
	if len(m.Answer) != 1 {
		t.Fatalf("wildcard: mau 1 answer, dapat %d", len(m.Answer))
	}
	if a, ok := m.Answer[0].(*mdns.A); !ok || a.A.String() != "8.8.8.8" {
		t.Fatalf("wildcard A salah: %v", m.Answer[0])
	}
}

// TestServeDNS_LongTXT_Packs memastikan nilai TXT >255 byte (mis. DKIM/SPF)
// dipecah jadi beberapa character-string sehingga m.Pack() tidak gagal.
func TestServeDNS_LongTXT_Packs(t *testing.T) {
	long := strings.Repeat("x", 300)
	fs := fakeStore{
		zones: []store.DNSZone{{ID: 1, Domain: "example.com", Serial: 1, Minimum: 3600}},
		recs: map[int64][]store.DNSRecord{1: {
			{ZoneID: 1, Name: "dkim", Type: "TXT", Value: long, TTL: 300},
		}},
	}
	snap, err := BuildSnapshot(fs)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	z := NewZones()
	z.Swap(snap)
	h := NewHandler(z)
	m := query(t, h, "dkim.example.com", mdns.TypeTXT)
	if len(m.Answer) != 1 {
		t.Fatalf("mau 1 TXT, dapat %d", len(m.Answer))
	}
	if _, err := m.Pack(); err != nil {
		t.Fatalf("TXT panjang harus bisa di-pack: %v", err)
	}
}

// TestServeDNS_ApexNS_Unconfigured: zona tanpa record NS (nameserver belum
// diset) harus tetap mengembalikan SOA di authority, bukan NOERROR kosong.
func TestServeDNS_ApexNS_Unconfigured(t *testing.T) {
	fs := fakeStore{
		zones: []store.DNSZone{{ID: 1, Domain: "example.com", Serial: 1, Minimum: 3600}},
		recs:  map[int64][]store.DNSRecord{1: {{ZoneID: 1, Name: "@", Type: "A", Value: "9.9.9.9", TTL: 300}}},
	}
	snap, _ := BuildSnapshot(fs)
	z := NewZones()
	z.Swap(snap)
	h := NewHandler(z)
	m := query(t, h, "example.com", mdns.TypeNS)
	if len(m.Answer) != 0 {
		t.Fatalf("tanpa NS: answer harus kosong, dapat %d", len(m.Answer))
	}
	if len(m.Ns) != 1 {
		t.Fatalf("harus ada SOA di authority, dapat %d", len(m.Ns))
	}
	if _, ok := m.Ns[0].(*mdns.SOA); !ok {
		t.Fatalf("authority harus SOA, dapat %T", m.Ns[0])
	}
}

// --- Test RRL (Response Rate Limiting) ---

// buildHandlerWithClock membuat Handler dengan seam waktu yang bisa dikontrol.
func buildHandlerWithClock(t *testing.T, nowFn func() time.Time) *Handler {
	t.Helper()
	snap, err := BuildSnapshot(fakeStore{
		zones: []store.DNSZone{{ID: 1, Domain: "example.com", Serial: 1, Minimum: 3600}},
		recs:  map[int64][]store.DNSRecord{1: {{ZoneID: 1, Name: "@", Type: "A", Value: "1.2.3.4", TTL: 300}}},
		set:   store.DNSSettings{NS1: "ns1.x"},
	})
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	z := NewZones()
	z.Swap(snap)
	h := NewHandler(z)
	h.now = nowFn
	return h
}

// sendQueryFrom mengirim satu query dari addr tertentu dan mengembalikan apakah ada balasan.
func sendQueryFrom(h *Handler, addr string) bool {
	req := new(mdns.Msg)
	req.SetQuestion(mdns.Fqdn("example.com"), mdns.TypeA)
	w := &capWriter{addr: fakeAddr{s: addr}}
	h.ServeDNS(w, req)
	return w.msg != nil
}

// TestServeDNS_RRL_DropsOverLimit memverifikasi bahwa query dari satu subnet
// yang melebihi batas rrlBurst dalam satu detik (waktu tetap) di-DROP.
func TestServeDNS_RRL_DropsOverLimit(t *testing.T) {
	// Waktu tetap — semua query terlihat pada detik yang sama.
	fixedTime := time.Unix(1000, 0)
	h := buildHandlerWithClock(t, func() time.Time { return fixedTime })

	const addr = "1.2.3.100:1234" // subnet 1.2.3.0/24
	// Kirim rrlBurst+10 query dari subnet yang sama; yang pertama rrlBurst harus lolos,
	// sisanya harus di-DROP karena token habis.
	total := int(rrlBurst) + 10
	answered := 0
	for i := 0; i < total; i++ {
		if sendQueryFrom(h, addr) {
			answered++
		}
	}

	if answered > int(rrlBurst) {
		t.Fatalf("lebih dari burst %d query seharusnya dijawab, dapat %d", int(rrlBurst), answered)
	}
	dropped := total - answered
	if dropped == 0 {
		t.Fatal("harus ada query yang di-DROP setelah burst terlampaui")
	}
	t.Logf("RRL: %d dijawab, %d di-DROP dari %d total (burst=%d, rate=%d/s)", answered, dropped, total, int(rrlBurst), int(rrlRate))
}

// TestServeDNS_RRL_DifferentSubnets memverifikasi bahwa subnet yang berbeda
// tidak saling mempengaruhi bucket RRL masing-masing.
func TestServeDNS_RRL_DifferentSubnets(t *testing.T) {
	fixedTime := time.Unix(2000, 0)
	h := buildHandlerWithClock(t, func() time.Time { return fixedTime })

	const (
		addrA = "10.0.1.50:9000" // subnet 10.0.1.0/24
		addrB = "10.0.2.50:9000" // subnet 10.0.2.0/24
	)

	// Habiskan semua token dari subnet A.
	for i := 0; i < int(rrlBurst)+5; i++ {
		sendQueryFrom(h, addrA)
	}

	// Subnet B belum pernah mengirim query — bucket-nya penuh; harus dijawab.
	if !sendQueryFrom(h, addrB) {
		t.Fatal("query dari subnet B yang belum pernah terlihat harus dijawab (bucket independen)")
	}
}

// TestServeDNS_RRL_RecoveryAfterTime memverifikasi bahwa token terisi ulang
// setelah waktu berlalu sehingga query kembali dijawab.
func TestServeDNS_RRL_RecoveryAfterTime(t *testing.T) {
	now := time.Unix(3000, 0)
	h := buildHandlerWithClock(t, func() time.Time { return now })

	const addr = "192.168.1.10:5353"

	// Habiskan semua token.
	for i := 0; i < int(rrlBurst)+5; i++ {
		sendQueryFrom(h, addr)
	}
	// Seharusnya sekarang di-DROP.
	if sendQueryFrom(h, addr) {
		t.Fatal("setelah burst habis, query harus di-DROP")
	}

	// Majukan waktu 2 detik → 2*rrlRate=40 token baru (memenuhi burst kembali).
	now = now.Add(2 * time.Second)

	// Sekarang harus lolos kembali.
	if !sendQueryFrom(h, addr) {
		t.Fatal("setelah 2 detik berlalu, token harus terisi ulang dan query harus dijawab")
	}
}

// --- Test ANY minimization (RFC 8482) ---

// TestServeDNS_ANY_MinimalHINFO memverifikasi bahwa query TypeANY terhadap nama
// yang di-host mengembalikan respons minimal (HINFO tunggal, bukan seluruh record set).
func TestServeDNS_ANY_MinimalHINFO(t *testing.T) {
	// Buat zona dengan beberapa record agar amplifikasi jelas terlihat bila ANY dikembalikan penuh.
	fs := fakeStore{
		zones: []store.DNSZone{{ID: 1, Domain: "example.com", Serial: 1, Minimum: 3600}},
		recs: map[int64][]store.DNSRecord{1: {
			{ZoneID: 1, Name: "@", Type: "A", Value: "1.1.1.1", TTL: 300},
			{ZoneID: 1, Name: "@", Type: "AAAA", Value: "2001:db8::1", TTL: 300},
			{ZoneID: 1, Name: "@", Type: "MX", Value: "mail.example.com", Priority: 10, TTL: 300},
			{ZoneID: 1, Name: "@", Type: "TXT", Value: "v=spf1 include:example.com ~all", TTL: 300},
		}},
		set: store.DNSSettings{NS1: "ns1.x"},
	}
	snap, err := BuildSnapshot(fs)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	z := NewZones()
	z.Swap(snap)
	h := NewHandler(z)

	req := new(mdns.Msg)
	req.SetQuestion(mdns.Fqdn("example.com"), mdns.TypeANY)
	w := &capWriter{}
	h.ServeDNS(w, req)

	if w.msg == nil {
		t.Fatal("harus ada balasan untuk query ANY")
	}
	m := w.msg
	if m.Rcode != mdns.RcodeSuccess {
		t.Fatalf("ANY harus NOERROR, dapat rcode=%d", m.Rcode)
	}
	// Harus ada tepat 1 answer (HINFO), BUKAN 4 record penuh.
	if len(m.Answer) != 1 {
		t.Fatalf("ANY harus menghasilkan 1 answer (HINFO RFC8482), dapat %d", len(m.Answer))
	}
	hinfo, ok := m.Answer[0].(*mdns.HINFO)
	if !ok {
		t.Fatalf("answer ANY harus HINFO, dapat %T", m.Answer[0])
	}
	if hinfo.Cpu != "RFC8482" {
		t.Fatalf("HINFO.Cpu harus 'RFC8482', dapat %q", hinfo.Cpu)
	}
}

// TestServeDNS_ANY_ForeignZone_StillRefused memastikan query ANY ke zona yang
// tidak di-host tetap dikembalikan REFUSED (bukan HINFO).
func TestServeDNS_ANY_ForeignZone_StillRefused(t *testing.T) {
	h := buildHandler(t)
	req := new(mdns.Msg)
	req.SetQuestion(mdns.Fqdn("nothosted.net"), mdns.TypeANY)
	w := &capWriter{}
	h.ServeDNS(w, req)
	if w.msg == nil {
		t.Fatal("harus ada balasan")
	}
	if w.msg.Rcode != mdns.RcodeRefused {
		t.Fatalf("ANY ke zona asing harus REFUSED, dapat %d", w.msg.Rcode)
	}
}
