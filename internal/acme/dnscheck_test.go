package acme

import (
	"context"
	"errors"
	"net"
	"testing"
)

type fakeResolver struct {
	ips []net.IP
	err error
}

func (f fakeResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	return f.ips, f.err
}

func TestDomainPointsHere(t *testing.T) {
	server := []net.IP{net.ParseIP("168.110.200.136")}
	ctx := context.Background()

	// domain mengarah ke IP server → true
	ok, err := DomainPointsHere(ctx, fakeResolver{ips: []net.IP{net.ParseIP("168.110.200.136")}}, "ada.com", server)
	if err != nil || !ok {
		t.Fatalf("harusnya true, dapat %v err=%v", ok, err)
	}

	// mengarah ke IP lain → false, tanpa error
	ok, err = DomainPointsHere(ctx, fakeResolver{ips: []net.IP{net.ParseIP("1.2.3.4")}}, "lain.com", server)
	if err != nil || ok {
		t.Fatalf("harusnya false, dapat %v err=%v", ok, err)
	}

	// NXDOMAIN / resolusi gagal → false + error
	ok, err = DomainPointsHere(ctx, fakeResolver{err: errors.New("NXDOMAIN")}, "nx.com", server)
	if err == nil || ok {
		t.Fatalf("harusnya false+error, dapat %v err=%v", ok, err)
	}

	// salah satu dari beberapa IP cocok → true
	ok, _ = DomainPointsHere(ctx, fakeResolver{ips: []net.IP{net.ParseIP("9.9.9.9"), net.ParseIP("168.110.200.136")}}, "multi.com", server)
	if !ok {
		t.Fatal("salah satu IP cocok harus true")
	}
}
