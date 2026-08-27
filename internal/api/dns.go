package api

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/lamun-my-id/lamund/internal/store"
)

func (s *server) registerDNS(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/dns/zones", s.requireAuth(s.handleListDNSZones))
	mux.HandleFunc("POST /api/v1/dns/zones", s.requireAuth(s.handleCreateDNSZone))
	mux.HandleFunc("GET /api/v1/dns/zones/{domain}", s.requireAuth(s.handleGetDNSZone))
	mux.HandleFunc("DELETE /api/v1/dns/zones/{domain}", s.requireAuth(s.handleDeleteDNSZone))
	mux.HandleFunc("GET /api/v1/dns/zones/{domain}/records", s.requireAuth(s.handleListDNSRecords))
	mux.HandleFunc("POST /api/v1/dns/zones/{domain}/records", s.requireAuth(s.handleAddDNSRecord))
	mux.HandleFunc("PATCH /api/v1/dns/zones/{domain}/records/{id}", s.requireAuth(s.handlePatchDNSRecord))
	mux.HandleFunc("DELETE /api/v1/dns/zones/{domain}/records/{id}", s.requireAuth(s.handleDeleteDNSRecord))
	mux.HandleFunc("GET /api/v1/dns/settings", s.requireAuth(s.handleGetDNSSettings))
	mux.HandleFunc("PUT /api/v1/dns/settings", s.requireAuth(s.handlePutDNSSettings))
	mux.HandleFunc("GET /api/v1/dns/zone-for", s.requireAuth(s.handleDNSZoneFor))
}

// handleDNSZoneFor menjawab apakah domain tercakup zona yang bisa diakses caller.
// Selalu 200; managed=true HANYA bila zona ditemukan DAN caller boleh aksesnya.
func (s *server) handleDNSZoneFor(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	domain := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("domain")))
	if domain == "" {
		writeErr(w, http.StatusBadRequest, "parameter domain wajib diisi")
		return
	}
	zone, label, ok := s.d.Store.FindDNSZoneForDomain(domain)
	managed := ok && s.canAccessOwner(u, zone.OwnerType, zone.OwnerID)
	zoneDomain := ""
	if ok {
		zoneDomain = zone.Domain
	}
	if !managed {
		label = ""
		zoneDomain = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"managed":   managed,
		"zone":      zoneDomain,
		"label":     label,
		"public_ip": s.d.PublicIP,
	})
}

// autoProvisionDNS membuat record A/AAAA untuk domain di zona yang dimiliki caller.
// Best-effort: tidak pernah mengembalikan error, tidak memblokir.
func (s *server) autoProvisionDNS(u *authUser, domain string) {
	zone, label, ok := s.d.Store.FindDNSZoneForDomain(domain)
	if !ok || !s.canAccessOwner(u, zone.OwnerType, zone.OwnerID) {
		return
	}
	s.provisionZoneRecords(zone, label)
}

// provisionZoneRecords menambah A/AAAA (per PublicIP) untuk label di zone, lalu
// sinyal reload bila ada yang baru. Bersama autoProvisionDNS & ...System.
func (s *server) provisionZoneRecords(zone *store.DNSZone, label string) {
	if s.d.PublicIP == "" {
		return
	}
	anyAdded := false
	for _, raw := range strings.Split(s.d.PublicIP, ",") {
		ip := strings.TrimSpace(raw)
		if ip == "" {
			continue
		}
		parsed := net.ParseIP(ip)
		if parsed == nil {
			continue
		}
		rtype := "AAAA"
		if parsed.To4() != nil {
			rtype = "A"
		}
		added, err := s.d.Store.AddDNSRecordIfAbsent(store.DNSRecord{
			ZoneID: zone.ID, Name: label, Type: rtype, Value: ip, TTL: 300,
		})
		if err != nil {
			log.Printf("provisionZoneRecords: gagal tambah %s %s %s: %v", label, rtype, ip, err)
			continue
		}
		if added {
			anyAdded = true
		}
	}
	if anyAdded {
		s.notifyReload()
	}
}

// ownedZone memuat zona DNS milik caller; superadmin boleh apa saja.
// Bila bukan milik caller (atau tak ada), balas 404 — jangan bocorkan keberadaan zona orang lain.
func (s *server) ownedZone(w http.ResponseWriter, r *http.Request) (*store.DNSZone, *authUser, bool) {
	u, _ := userFrom(r.Context())
	domain := r.PathValue("domain")
	zone, err := s.d.Store.GetDNSZone(domain)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca zona")
		return nil, nil, false
	}
	if zone == nil || !s.canAccessOwner(u, zone.OwnerType, zone.OwnerID) {
		writeErr(w, http.StatusNotFound, "zona tidak ditemukan")
		return nil, nil, false
	}
	return zone, u, true
}

// --- List zones ---

type dnsZoneJSON struct {
	ID          int64  `json:"id"`
	Domain      string `json:"domain"`
	OwnerType   string `json:"owner_type"`
	OwnerID     int64  `json:"owner_id"`
	Serial      int64  `json:"serial"`
	RecordCount int    `json:"record_count"`
	CreatedAt   string `json:"created_at"`
}

func (s *server) handleListDNSZones(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	var (
		zones []store.DNSZone
		err   error
	)
	if u.Role == "superadmin" {
		zones, err = s.d.Store.ListDNSZones()
	} else {
		zones, err = s.d.Store.ListDNSZonesVisibleTo(u.ID)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca zona")
		return
	}
	out := make([]dnsZoneJSON, 0, len(zones))
	for _, z := range zones {
		recs, _ := s.d.Store.ListDNSRecords(z.ID)
		out = append(out, dnsZoneJSON{
			ID:          z.ID,
			Domain:      z.Domain,
			OwnerType:   z.OwnerType,
			OwnerID:     z.OwnerID,
			Serial:      z.Serial,
			RecordCount: len(recs),
			CreatedAt:   z.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"zones": out})
}

// --- Create zone ---

type createDNSZoneReq struct {
	Domain    string `json:"domain"`
	OwnerType string `json:"owner_type"`
	OwnerID   int64  `json:"owner_id"`
}

func (s *server) handleCreateDNSZone(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	var req createDNSZoneReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
	if err := store.ValidateDomain(req.Domain); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ownerType, ownerID, okOwner := s.resolveOwner(u, req.OwnerType, req.OwnerID)
	if !okOwner {
		writeErr(w, http.StatusForbidden, "kamu bukan anggota tim tujuan")
		return
	}
	zone := store.DNSZone{
		Domain:    req.Domain,
		OwnerType: ownerType,
		OwnerID:   ownerID,
		UserID:    u.ID,
	}
	id, err := s.d.Store.CreateDNSZone(zone)
	if err != nil {
		// ValidateDomain sudah dicek di atas, jadi error di sini bukan validasi:
		// "terdaftar" → 409, sisanya error store nyata → 500 (jangan bocorkan).
		if strings.Contains(err.Error(), "terdaftar") {
			writeErr(w, http.StatusConflict, err.Error())
		} else {
			writeErr(w, http.StatusInternalServerError, "gagal membuat zona")
		}
		return
	}
	s.notifyReload()
	created, _ := s.d.Store.GetDNSZone(req.Domain)
	recs, _ := s.d.Store.ListDNSRecords(id)
	writeJSON(w, http.StatusCreated, map[string]any{
		"zone":    created,
		"records": recs,
	})
}

// --- Get zone ---

type glueRecord struct {
	Host string `json:"host"`
	IP   string `json:"ip"`
}

func (s *server) handleGetDNSZone(w http.ResponseWriter, r *http.Request) {
	zone, _, ok := s.ownedZone(w, r)
	if !ok {
		return
	}
	recs, err := s.d.Store.ListDNSRecords(zone.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca record")
		return
	}
	settings, err := s.d.Store.GetDNSSettings()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca setelan DNS")
		return
	}
	nameservers := []string{}
	if settings.NS1 != "" {
		nameservers = append(nameservers, settings.NS1)
	}
	if settings.NS2 != "" {
		nameservers = append(nameservers, settings.NS2)
	}
	glue := []glueRecord{}
	publicIP := s.d.PublicIP
	for _, ns := range nameservers {
		glue = append(glue, glueRecord{Host: ns, IP: publicIP})
	}
	if recs == nil {
		recs = []store.DNSRecord{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"zone":        zone,
		"records":     recs,
		"nameservers": nameservers,
		"glue":        glue,
	})
}

// --- Delete zone ---

func (s *server) handleDeleteDNSZone(w http.ResponseWriter, r *http.Request) {
	zone, _, ok := s.ownedZone(w, r)
	if !ok {
		return
	}
	if err := s.d.Store.DeleteDNSZone(zone.Domain); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menghapus zona")
		return
	}
	s.notifyReload()
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- List records ---

func (s *server) handleListDNSRecords(w http.ResponseWriter, r *http.Request) {
	zone, _, ok := s.ownedZone(w, r)
	if !ok {
		return
	}
	recs, err := s.d.Store.ListDNSRecords(zone.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca record")
		return
	}
	if recs == nil {
		recs = []store.DNSRecord{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": recs})
}

// --- Add record ---

type addDNSRecordReq struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	TTL      int64  `json:"ttl"`
	Priority int64  `json:"priority"`
}

func (s *server) handleAddDNSRecord(w http.ResponseWriter, r *http.Request) {
	zone, _, ok := s.ownedZone(w, r)
	if !ok {
		return
	}
	var req addDNSRecordReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	req.Name = strings.ToLower(strings.TrimSpace(req.Name))
	req.Type = strings.ToUpper(strings.TrimSpace(req.Type))
	if req.TTL == 0 {
		req.TTL = 3600
	}
	if err := validateDNSRecord(req.Type, req.Name, req.Value, req.Priority); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	_, err := s.d.Store.AddDNSRecord(store.DNSRecord{
		ZoneID:   zone.ID,
		Name:     req.Name,
		Type:     req.Type,
		Value:    req.Value,
		TTL:      req.TTL,
		Priority: req.Priority,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menyimpan record")
		return
	}
	s.notifyReload()
	recs, _ := s.d.Store.ListDNSRecords(zone.ID)
	if recs == nil {
		recs = []store.DNSRecord{}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"records": recs})
}

// --- Patch record ---

type patchDNSRecordReq struct {
	Value    string `json:"value"`
	TTL      int64  `json:"ttl"`
	Priority int64  `json:"priority"`
}

func (s *server) handlePatchDNSRecord(w http.ResponseWriter, r *http.Request) {
	zone, _, ok := s.ownedZone(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var req patchDNSRecordReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	// Ambil record yang ada agar bisa memvalidasi ulang nilai baru terhadap tipe
	// tersimpan (PATCH tak mengirim tipe). Sekaligus jadi cek keberadaan → 404.
	recs, err := s.d.Store.ListDNSRecords(zone.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca record")
		return
	}
	var existing *store.DNSRecord
	for i := range recs {
		if recs[i].ID == id {
			existing = &recs[i]
			break
		}
	}
	if existing == nil {
		writeErr(w, http.StatusNotFound, "record tidak ditemukan")
		return
	}
	if err := validateDNSRecord(existing.Type, existing.Name, req.Value, req.Priority); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.d.Store.UpdateDNSRecord(zone.ID, id, req.Value, req.TTL, req.Priority); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menyimpan record")
		return
	}
	s.notifyReload()
	recs, _ = s.d.Store.ListDNSRecords(zone.ID)
	if recs == nil {
		recs = []store.DNSRecord{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": recs})
}

// --- Delete record ---

func (s *server) handleDeleteDNSRecord(w http.ResponseWriter, r *http.Request) {
	zone, _, ok := s.ownedZone(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}

	// Cek apakah record ini NS apex dan apakah menghapusnya akan meninggalkan 0 NS.
	recs, err := s.d.Store.ListDNSRecords(zone.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca record")
		return
	}
	// Temukan record yang hendak dihapus.
	var target *store.DNSRecord
	for i := range recs {
		if recs[i].ID == id {
			target = &recs[i]
			break
		}
	}
	if target != nil && target.Type == "NS" && target.Name == "@" {
		// Hitung NS apex yang tersisa setelah dihapus.
		nsCount := 0
		for _, rc := range recs {
			if rc.Type == "NS" && rc.Name == "@" && rc.ID != id {
				nsCount++
			}
		}
		if nsCount == 0 {
			writeErr(w, http.StatusBadRequest, "zona harus punya minimal 1 NS")
			return
		}
	}

	// target != nil dari cek di atas (bila nil, DeleteDNSRecord no-op aman).
	if err := s.d.Store.DeleteDNSRecord(zone.ID, id); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menghapus record")
		return
	}
	s.notifyReload()
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- DNS settings ---

func (s *server) handleGetDNSSettings(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	if u.Role != "superadmin" {
		writeErr(w, http.StatusForbidden, "butuh hak superadmin")
		return
	}
	settings, err := s.d.Store.GetDNSSettings()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca setelan DNS")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ns1":        settings.NS1,
		"ns2":        settings.NS2,
		"hostmaster": settings.Hostmaster,
		"public_ip":  s.d.PublicIP,
	})
}

type putDNSSettingsReq struct {
	NS1        string `json:"ns1"`
	NS2        string `json:"ns2"`
	Hostmaster string `json:"hostmaster"`
}

func (s *server) handlePutDNSSettings(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r.Context())
	if u.Role != "superadmin" {
		writeErr(w, http.StatusForbidden, "butuh hak superadmin")
		return
	}
	var req putDNSSettingsReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	// Validasi: ns1/ns2/hostmaster harus hostname valid (kosong diizinkan).
	if req.NS1 != "" {
		if err := store.ValidateDomain(strings.TrimSuffix(req.NS1, ".")); err != nil {
			writeErr(w, http.StatusBadRequest, "ns1 tidak valid: "+err.Error())
			return
		}
	}
	if req.NS2 != "" {
		if err := store.ValidateDomain(strings.TrimSuffix(req.NS2, ".")); err != nil {
			writeErr(w, http.StatusBadRequest, "ns2 tidak valid: "+err.Error())
			return
		}
	}
	if req.Hostmaster != "" {
		if err := store.ValidateDomain(strings.TrimSuffix(req.Hostmaster, ".")); err != nil {
			writeErr(w, http.StatusBadRequest, "hostmaster tidak valid: "+err.Error())
			return
		}
	}
	if err := s.d.Store.SetDNSSettings(store.DNSSettings{
		NS1:        req.NS1,
		NS2:        req.NS2,
		Hostmaster: req.Hostmaster,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menyimpan setelan DNS")
		return
	}
	s.notifyReload()
	writeJSON(w, http.StatusOK, map[string]any{
		"ns1":        req.NS1,
		"ns2":        req.NS2,
		"hostmaster": req.Hostmaster,
		"public_ip":  s.d.PublicIP,
	})
}

// --- Validasi record DNS ---

// labelRe memvalidasi nama record DNS relatif: @, *, wildcard `*.sub`, atau
// nama satu/banyak label (RFC-1123, mengizinkan underscore untuk _dmarc dst).
var labelRe = regexp.MustCompile(`^(@|\*(\.[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?)*|[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?(\.[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?)*)$`)

// caaRe memvalidasi format nilai CAA: flags tag "value" (di-anchor penuh).
var caaRe = regexp.MustCompile(`^\d+\s+\w+\s+.+$`)

// validateDNSRecord memvalidasi tipe dan nilai record DNS.
// rtype harus sudah di-uppercase sebelum dipanggil.
func validateDNSRecord(rtype, name, value string, priority int64) error {
	// Validasi nama record.
	if !labelRe.MatchString(name) {
		return fmt.Errorf("nama record tidak valid: %q", name)
	}

	switch rtype {
	case "A":
		ip := net.ParseIP(value)
		if ip == nil || ip.To4() == nil {
			return fmt.Errorf("nilai A harus berupa IPv4 yang valid")
		}
	case "AAAA":
		ip := net.ParseIP(value)
		if ip == nil || ip.To4() != nil {
			return fmt.Errorf("nilai AAAA harus berupa IPv6 yang valid")
		}
	case "CNAME":
		if name == "@" {
			return fmt.Errorf("CNAME tidak boleh di apex (@)")
		}
		cleaned := strings.TrimSuffix(value, ".")
		if err := store.ValidateDomain(cleaned); err != nil {
			return fmt.Errorf("nilai CNAME tidak valid: %w", err)
		}
	case "NS":
		cleaned := strings.TrimSuffix(value, ".")
		if err := store.ValidateDomain(cleaned); err != nil {
			return fmt.Errorf("nilai NS tidak valid: %w", err)
		}
	case "MX":
		cleaned := strings.TrimSuffix(value, ".")
		if err := store.ValidateDomain(cleaned); err != nil {
			return fmt.Errorf("nilai MX tidak valid: %w", err)
		}
		if priority < 0 {
			return fmt.Errorf("priority MX harus >= 0")
		}
	case "TXT":
		if len(value) > 65535 {
			return fmt.Errorf("nilai TXT terlalu panjang (maks 65535 karakter)")
		}
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("nilai TXT tidak boleh mengandung CR/LF")
		}
	case "CAA":
		if !caaRe.MatchString(value) {
			return fmt.Errorf("nilai CAA harus dalam format: flags tag value (mis. 0 issue \"letsencrypt.org\")")
		}
		// Validasi flags 0-255.
		parts := strings.SplitN(value, " ", 3)
		flags, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || flags < 0 || flags > 255 {
			return fmt.Errorf("flags CAA harus 0-255")
		}
	default:
		return fmt.Errorf("tipe record %q tidak didukung", rtype)
	}
	return nil
}
