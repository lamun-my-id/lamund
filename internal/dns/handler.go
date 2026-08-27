package dns

import (
	"net"
	"strings"
	"sync"
	"time"

	mdns "github.com/miekg/dns"
)

// Handler mengimplementasikan dns.Handler dan menjawab query secara authoritative.
// Tak pernah merekursi — query zona yang tak di-host dikembalikan REFUSED.
type Handler struct {
	Z   *Zones
	now func() time.Time // seam untuk test; nil → time.Now

	rrlMu     sync.Mutex
	rrlBucket map[string]*rrlBucket
}

// NewHandler membuat Handler dengan RRL ter-inisialisasi.
// Gunakan ini sebagai pengganti &Handler{Z: z} bila Anda ingin Handler yang
// siap pakai; &Handler{Z: z} juga aman karena rrlBucket di-lazy-init.
func NewHandler(z *Zones) *Handler {
	return &Handler{
		Z:         z,
		rrlBucket: make(map[string]*rrlBucket),
	}
}

// rrlBucket adalah token-bucket sederhana untuk satu subnet.
type rrlBucket struct {
	tokens   float64
	lastSeen time.Time
}

// rrlRate adalah jumlah token (respons) per detik per subnet.
const rrlRate = 20.0

// rrlBurst adalah kapasitas maksimal token (burst) per subnet.
const rrlBurst = 40.0

// rrlMaxBuckets adalah batas ukuran map bucket — eviksi lazy bila terlampaui.
const rrlMaxBuckets = 4096

// nowFn mengembalikan fungsi waktu aktif (seam atau time.Now).
func (h *Handler) nowFn() func() time.Time {
	if h.now != nil {
		return h.now
	}
	return time.Now
}

// rrlSubnetKey mengekstrak kunci subnet dari addr RemoteAddr.
// IPv4 → /24 prefix; IPv6 → /64 prefix; gagal parse → addr mentah.
func rrlSubnetKey(addr net.Addr) string {
	addrStr := addr.String()
	host, _, err := net.SplitHostPort(addrStr)
	if err != nil {
		// Mungkin tidak ada port (jarang), pakai apa adanya.
		host = addrStr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return host
	}
	if ip4 := ip.To4(); ip4 != nil {
		// IPv4 /24
		return ip4[0:3].String() + ".0/24"
	}
	// IPv6 /64
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// rrlAllow mengembalikan true bila query dari subnet ini boleh diproses.
// Bila false, query harus di-DROP (jangan tulis respons).
func (h *Handler) rrlAllow(addr net.Addr) bool {
	key := rrlSubnetKey(addr)
	now := h.nowFn()()

	h.rrlMu.Lock()
	defer h.rrlMu.Unlock()

	// Lazy-init map.
	if h.rrlBucket == nil {
		h.rrlBucket = make(map[string]*rrlBucket)
	}

	b, exists := h.rrlBucket[key]
	if !exists {
		// Eviksi lazy: bila map terlalu besar, buang semua bucket lama (>5 detik).
		if len(h.rrlBucket) >= rrlMaxBuckets {
			h.rrlEvict(now)
		}
		b = &rrlBucket{tokens: rrlBurst, lastSeen: now}
		h.rrlBucket[key] = b
	}

	// Isi ulang token berdasarkan waktu yang telah berlalu.
	elapsed := now.Sub(b.lastSeen).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * rrlRate
		if b.tokens > rrlBurst {
			b.tokens = rrlBurst
		}
		b.lastSeen = now
	}

	if b.tokens >= 1.0 {
		b.tokens--
		return true
	}
	return false
}

// rrlEvict membuang bucket yang tidak aktif selama >5 detik.
// Harus dipanggil dengan rrlMu terkunci.
func (h *Handler) rrlEvict(now time.Time) {
	cutoff := now.Add(-5 * time.Second)
	for k, b := range h.rrlBucket {
		if b.lastSeen.Before(cutoff) {
			delete(h.rrlBucket, k)
		}
	}
}

// ServeDNS memproses satu DNS query.
//
// Semantik:
//   - Zona tak dikenal → REFUSED (bukan SERVFAIL — kami bukan resolver)
//   - Apex SOA / apex NS → jawab langsung dari zona
//   - Exact name+type match → answer
//   - Wildcard '*' bila exact tidak ada
//   - NODATA (nama ada, type tidak) → NOERROR + SOA di authority
//   - NXDOMAIN (nama tidak ada) → RcodeNameError + SOA di authority
//   - CNAME fallback bila qtype≠CNAME tapi CNAME ada di nama
//   - TypeANY → respons minimal HINFO (RFC 8482) untuk zona yang di-host
//   - RRL: query dari subnet yang melampaui 20/detik di-DROP (UDP amplification)
func (h *Handler) ServeDNS(w mdns.ResponseWriter, req *mdns.Msg) {
	m := new(mdns.Msg)
	m.SetReply(req)
	m.Authoritative = true
	m.RecursionAvailable = false

	// Tolak query multi-question atau non-INET.
	if len(req.Question) != 1 || req.Question[0].Qclass != mdns.ClassINET {
		m.SetRcode(req, mdns.RcodeRefused)
		_ = w.WriteMsg(m)
		return
	}

	q := req.Question[0]
	qname := strings.ToLower(mdns.Fqdn(q.Name))

	zone := h.Z.Find(qname)
	if zone == nil {
		// Zona tidak kami hosting → REFUSED (cegah amplifikasi DNS)
		m.SetRcode(req, mdns.RcodeRefused)
		_ = w.WriteMsg(m)
		return
	}

	// Response Rate Limiting (RRL): DROP bila subnet melebihi batas.
	// Diterapkan setelah cek zona agar REFUSED zona asing tetap bisa diproses
	// (kecil, rendah amplifikasi), sementara respons besar zona yang di-host
	// dibatasi.
	if !h.rrlAllow(w.RemoteAddr()) {
		// DROP — jangan tulis respons sama sekali (mengalahkan amplifikasi refleksi)
		return
	}

	// Set EDNS bila client memintanya.
	if req.IsEdns0() != nil {
		m.SetEdns0(1232, false)
	}

	apex := mdns.Fqdn(zone.Apex)

	// --- TypeANY minimization (RFC 8482) ---
	// Kembalikan HINFO tunggal alih-alih seluruh set record — kurangi faktor amplifikasi.
	if q.Qtype == mdns.TypeANY {
		hinfo := &mdns.HINFO{
			Hdr: mdns.RR_Header{
				Name:   qname,
				Rrtype: mdns.TypeHINFO,
				Class:  mdns.ClassINET,
				Ttl:    zone.minimum,
			},
			Cpu: "RFC8482",
			Os:  "",
		}
		m.Answer = append(m.Answer, hinfo)
		_ = w.WriteMsg(m)
		return
	}

	// --- Apex SOA ---
	if qname == apex && q.Qtype == mdns.TypeSOA {
		m.Answer = append(m.Answer, zone.SOA())
		_ = w.WriteMsg(m)
		return
	}

	// --- Apex NS ---
	// Hanya jawab langsung bila zona benar-benar punya record NS. Zona yang
	// belum dikonfigurasi (nameserver kosong) jatuh ke jalur NODATA di bawah
	// agar SOA tetap muncul di authority (bukan NOERROR kosong tanpa SOA).
	if qname == apex && q.Qtype == mdns.TypeNS {
		if ns := zone.NS(); len(ns) > 0 {
			m.Answer = append(m.Answer, ns...)
			_ = w.WriteMsg(m)
			return
		}
	}

	// --- Exact match atau wildcard ---
	ans, matchedName := zone.Match(qname, q.Qtype)
	if len(ans) > 0 {
		m.Answer = ans
		_ = w.WriteMsg(m)
		return
	}

	// --- CNAME fallback: qtype != CNAME tapi ada CNAME di nama ini ---
	// (matchedName sudah true bila nama punya record apa pun, termasuk CNAME.)
	if q.Qtype != mdns.TypeCNAME {
		if cn, _ := zone.Match(qname, mdns.TypeCNAME); len(cn) > 0 {
			m.Answer = cn
			_ = w.WriteMsg(m)
			return
		}
	}

	// --- NODATA atau NXDOMAIN: selalu sertakan SOA di authority ---
	m.Ns = append(m.Ns, zone.SOA())
	if !matchedName {
		m.Rcode = mdns.RcodeNameError // NXDOMAIN
	}
	// NODATA: m.Rcode tetap RcodeSuccess (default dari SetReply)

	_ = w.WriteMsg(m)
}
