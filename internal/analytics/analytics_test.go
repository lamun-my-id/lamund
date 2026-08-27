package analytics

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lamun-my-id/lamund/internal/logging"
)

// writeLog menulis beberapa Entry ke <dir>/<domain>.log.
func writeLog(t *testing.T, dir, domain string, entries []logging.Entry) {
	t.Helper()
	var buf []byte
	for _, e := range entries {
		buf = append(buf, []byte(e.Format())...)
	}
	if err := os.WriteFile(filepath.Join(dir, domain+".log"), buf, 0o640); err != nil {
		t.Fatal(err)
	}
}

func TestOverviewAggregates(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-1 * time.Hour)
	old := now.Add(-48 * time.Hour) // di luar jendela 24 jam

	writeLog(t, dir, "a.test", []logging.Entry{
		{Time: recent, Domain: "a.test", Method: "GET", Path: "/", Status: 200, Bytes: 100},
		{Time: recent, Domain: "a.test", Method: "GET", Path: "/x", Status: 500, Bytes: 10},
		{Time: old, Domain: "a.test", Method: "GET", Path: "/old", Status: 200, Bytes: 999},
	})
	writeLog(t, dir, "b.test", []logging.Entry{
		{Time: recent, Domain: "b.test", Method: "GET", Path: "/", Status: 200, Bytes: 50},
	})

	ov := ComputeOverview(dir, now, nil)
	if ov.TotalRequests != 3 {
		t.Fatalf("total request 24 jam harus 3 (old dikecualikan), dapat %d", ov.TotalRequests)
	}
	if ov.TotalErrors != 1 {
		t.Fatalf("total error harus 1, dapat %d", ov.TotalErrors)
	}
	if len(ov.TopAccessed) == 0 || ov.TopAccessed[0].Domain != "a.test" {
		t.Fatalf("top accessed harus a.test dulu: %+v", ov.TopAccessed)
	}
	if len(ov.RecentErrors) != 1 || ov.RecentErrors[0].Status != 500 {
		t.Fatalf("recent errors salah: %+v", ov.RecentErrors)
	}
}

func TestOverviewScoping(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-30 * time.Minute)
	writeLog(t, dir, "a.test", []logging.Entry{{Time: recent, Domain: "a.test", Method: "GET", Path: "/", Status: 200, Bytes: 1}})
	writeLog(t, dir, "b.test", []logging.Entry{{Time: recent, Domain: "b.test", Method: "GET", Path: "/", Status: 200, Bytes: 1}})

	only := map[string]bool{"a.test": true}
	ov := ComputeOverview(dir, now, only)
	if len(ov.TopAccessed) != 1 || ov.TopAccessed[0].Domain != "a.test" {
		t.Fatalf("scoping harus batasi ke a.test saja: %+v", ov.TopAccessed)
	}
}

func TestDomainSeries(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 23, 12, 30, 0, 0, time.UTC)
	writeLog(t, dir, "a.test", []logging.Entry{
		{Time: now.Add(-90 * time.Minute), Domain: "a.test", Method: "GET", Path: "/", Status: 200, Bytes: 5},
		{Time: now.Add(-10 * time.Minute), Domain: "a.test", Method: "GET", Path: "/", Status: 404, Bytes: 3},
	})
	rep := ComputeDomain(dir, "a.test", now)
	if rep.Requests != 2 {
		t.Fatalf("requests harus 2, dapat %d", rep.Requests)
	}
	if rep.Errors != 1 {
		t.Fatalf("errors harus 1, dapat %d", rep.Errors)
	}
	if len(rep.Series) != 24 {
		t.Fatalf("series harus 24 bucket jam, dapat %d", len(rep.Series))
	}
}
