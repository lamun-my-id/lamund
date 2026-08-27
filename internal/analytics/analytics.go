// Package analytics mengagregasi access log per-domain (yang ditulis
// internal/logging) menjadi ringkasan trafik & error untuk panel. Read-only:
// aman dipanggil control plane yang hanya membaca berkas log.
package analytics

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DomainStat = agregat satu domain dalam jendela waktu.
type DomainStat struct {
	Domain   string `json:"domain"`
	Requests int    `json:"requests"`
	Bytes    int64  `json:"bytes"`
	Errors   int    `json:"errors"` // status >= 400
}

// ErrorEntry = satu request 4xx/5xx (untuk daftar "error terakhir").
type ErrorEntry struct {
	Time   string `json:"time"`
	Domain string `json:"domain"`
	Path   string `json:"path"`
	Status int    `json:"status"`
}

// HourPoint = bucket per-jam untuk sparkline trafik.
type HourPoint struct {
	Hour     string `json:"hour"` // "2006-01-02T15"
	Requests int    `json:"requests"`
	Bytes    int64  `json:"bytes"`
}

// Overview = ringkasan lintas domain untuk halaman utama.
type Overview struct {
	TotalRequests int          `json:"total_requests"`
	TotalBytes    int64        `json:"total_bytes"`
	TotalErrors   int          `json:"total_errors"`
	TopAccessed   []DomainStat `json:"top_accessed"`  // top-5 by requests
	RecentErrors  []ErrorEntry `json:"recent_errors"` // 5 terbaru
}

// DomainReport = analitik satu deployment.
type DomainReport struct {
	Domain       string       `json:"domain"`
	Requests     int          `json:"requests"`
	Bytes        int64        `json:"bytes"`
	Errors       int          `json:"errors"`
	Series       []HourPoint  `json:"series"`        // 24 jam terakhir
	RecentErrors []ErrorEntry `json:"recent_errors"` // 10 terbaru
}

// line memparse satu baris log TAB-separated:
// time \t domain \t remote \t method \t path \t status \t bytes
type line struct {
	t      time.Time
	domain string
	path   string
	status int
	bytes  int64
}

func parseLine(s string) (line, bool) {
	f := strings.Split(s, "\t")
	if len(f) < 7 {
		return line{}, false
	}
	t, err := time.Parse(time.RFC3339, f[0])
	if err != nil {
		return line{}, false
	}
	status, _ := strconv.Atoi(f[5])
	bytes, _ := strconv.ParseInt(f[6], 10, 64)
	return line{t: t, domain: f[1], path: f[4], status: status, bytes: bytes}, true
}

// scanDomain memanggil fn untuk tiap baris log domain (file aktif + rotasi .1)
// yang waktunya >= since. Berkas tak ada → dilewati diam-diam.
func scanDomain(dir, domain string, since time.Time, fn func(line)) {
	for _, name := range []string{safeName(domain) + ".log.1", safeName(domain) + ".log"} {
		scanFile(filepath.Join(dir, name), since, fn)
	}
}

func scanFile(path string, since time.Time, fn func(line)) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		l, ok := parseLine(sc.Text())
		if !ok || l.t.Before(since) {
			continue
		}
		fn(l)
	}
}

// domainsIn mengembalikan daftar domain yang punya berkas log di dir.
func domainsIn(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".log") {
			continue
		}
		d := strings.TrimSuffix(n, ".log")
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

// ComputeOverview mengagregasi seluruh (atau subset `only`) domain dalam 24 jam
// terakhir. only=nil → semua domain (dipakai superadmin). only berisi set nama
// domain yang boleh dilihat caller.
func ComputeOverview(dir string, now time.Time, only map[string]bool) Overview {
	since := now.Add(-24 * time.Hour)
	var ov Overview
	var errs []ErrorEntry
	for _, d := range domainsIn(dir) {
		if only != nil && !only[d] {
			continue
		}
		st := DomainStat{Domain: d}
		scanDomain(dir, d, since, func(l line) {
			st.Requests++
			st.Bytes += l.bytes
			if l.status >= 400 {
				st.Errors++
				errs = append(errs, ErrorEntry{Time: l.t.Format(time.RFC3339), Domain: d, Path: l.path, Status: l.status})
			}
		})
		if st.Requests == 0 {
			continue
		}
		ov.TotalRequests += st.Requests
		ov.TotalBytes += st.Bytes
		ov.TotalErrors += st.Errors
		ov.TopAccessed = append(ov.TopAccessed, st)
	}
	sort.Slice(ov.TopAccessed, func(i, j int) bool { return ov.TopAccessed[i].Requests > ov.TopAccessed[j].Requests })
	ov.TopAccessed = topN(ov.TopAccessed, 5)
	ov.RecentErrors = lastN(errs, 5)
	if ov.TopAccessed == nil {
		ov.TopAccessed = []DomainStat{}
	}
	if ov.RecentErrors == nil {
		ov.RecentErrors = []ErrorEntry{}
	}
	return ov
}

// ComputeDomain menghasilkan laporan 24 jam untuk satu domain (sparkline per-jam).
func ComputeDomain(dir, domain string, now time.Time) DomainReport {
	since := now.Add(-24 * time.Hour).Truncate(time.Hour)
	buckets := map[string]*HourPoint{}
	var order []string
	for h := 0; h < 24; h++ {
		key := since.Add(time.Duration(h) * time.Hour).Format("2006-01-02T15")
		buckets[key] = &HourPoint{Hour: key}
		order = append(order, key)
	}
	rep := DomainReport{Domain: domain}
	var errs []ErrorEntry
	scanDomain(dir, domain, since, func(l line) {
		rep.Requests++
		rep.Bytes += l.bytes
		key := l.t.Format("2006-01-02T15")
		if b := buckets[key]; b != nil {
			b.Requests++
			b.Bytes += l.bytes
		}
		if l.status >= 400 {
			rep.Errors++
			errs = append(errs, ErrorEntry{Time: l.t.Format(time.RFC3339), Domain: domain, Path: l.path, Status: l.status})
		}
	})
	for _, k := range order {
		rep.Series = append(rep.Series, *buckets[k])
	}
	rep.RecentErrors = lastN(errs, 10)
	if rep.RecentErrors == nil {
		rep.RecentErrors = []ErrorEntry{}
	}
	return rep
}

func topN[T any](s []T, n int) []T {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// lastN mengembalikan n elemen terakhir (paling baru diasumsikan di akhir).
func lastN(s []ErrorEntry, n int) []ErrorEntry {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// safeName mereplikasi logging.safeName (nama file log) tanpa import silang.
func safeName(domain string) string {
	d := strings.ToLower(strings.TrimSpace(domain))
	d = strings.ReplaceAll(d, "/", "_")
	d = strings.ReplaceAll(d, "..", "_")
	if d == "" {
		return "_unknown"
	}
	return d
}
