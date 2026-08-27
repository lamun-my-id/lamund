// Package acme mengurus penerbitan & pembaruan sertifikat HTTPS otomatis.
package acme

import (
	"context"
	"fmt"
	"net"
)

// Resolver adalah seam DNS agar bisa di-test (dipenuhi *net.Resolver).
type Resolver interface {
	LookupIP(ctx context.Context, network, host string) ([]net.IP, error)
}

// DomainPointsHere memastikan A/AAAA domain mengarah ke salah satu IP server
// SEBELUM meminta sertifikat — mencegah percobaan sia-sia yang membakar
// rate-limit Let's Encrypt (PRD §6.1). false+error = resolusi gagal;
// false+nil = mengarah ke tempat lain.
func DomainPointsHere(ctx context.Context, r Resolver, domain string, serverIPs []net.IP) (bool, error) {
	ips, err := r.LookupIP(ctx, "ip", domain)
	if err != nil {
		return false, fmt.Errorf("resolusi DNS %s gagal: %w", domain, err)
	}
	for _, got := range ips {
		for _, want := range serverIPs {
			if got.Equal(want) {
				return true, nil
			}
		}
	}
	return false, nil
}
