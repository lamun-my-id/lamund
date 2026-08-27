package store

import (
	"fmt"
	"strings"
)

// ValidateDomain menegakkan aturan PRD §6.1: lowercase RFC-1123, tanpa
// wildcard, minimal satu titik. Domain adalah pintu masuk paling rawan produk
// ini — lebih baik menolak berlebihan daripada lolos satu.
func ValidateDomain(d string) error {
	if len(d) == 0 || len(d) > 253 {
		return fmt.Errorf("panjang domain harus 1-253 karakter")
	}
	if strings.Contains(d, "*") {
		return fmt.Errorf("wildcard belum didukung")
	}
	labels := strings.Split(d, ".")
	if len(labels) < 2 {
		return fmt.Errorf("domain harus punya minimal satu titik")
	}
	for _, l := range labels {
		if len(l) == 0 || len(l) > 63 {
			return fmt.Errorf("label harus 1-63 karakter")
		}
		if l[0] == '-' || l[len(l)-1] == '-' {
			return fmt.Errorf("label tidak boleh diawali/diakhiri '-'")
		}
		for _, c := range l {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return fmt.Errorf("karakter %q tidak diizinkan (pakai lowercase)", c)
			}
		}
	}
	return nil
}
