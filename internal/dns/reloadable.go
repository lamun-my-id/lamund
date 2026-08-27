package dns

import (
	"net"
	"time"

	mdns "github.com/miekg/dns"
	"golang.org/x/net/netutil"
)

// NewReloadableZones membangun snapshot awal dari store dan menyimpannya di Zones.
// Error dikembalikan bila build snapshot gagal (Zones tidak dapat dipakai).
func NewReloadableZones(st Store) (*Zones, error) {
	snap, err := BuildSnapshot(st)
	if err != nil {
		return nil, err
	}
	z := NewZones()
	z.Swap(snap)
	return z, nil
}

// Reload membangun snapshot baru dari store dan menggantinya secara atomik.
// Bila build gagal, snapshot lama dipertahankan dan error dikembalikan —
// zona yang sedang dilayani tidak pernah dijatuhkan hanya karena reload gagal.
func (z *Zones) Reload(st Store) error {
	snap, err := BuildSnapshot(st)
	if err != nil {
		return err
	}
	z.Swap(snap)
	return nil
}

// tcpMaxConns adalah batas koneksi TCP konkuren untuk mencegah exhaustion.
const tcpMaxConns = 256

// Serve memulai DNS server authoritative pada addr untuk kedua protokol UDP dan TCP.
// Handler menggunakan z sebagai sumber zona aktif.
// Koneksi TCP dibatasi hingga tcpMaxConns konkuren (via netutil.LimitListener).
// Mengembalikan fungsi stop yang mematikan kedua server saat dipanggil.
func Serve(addr string, z *Zones) (func() error, error) {
	h := NewHandler(z)

	// Buat TCP listener eksplisit agar bisa dibungkus LimitListener.
	tcpLn, err := net.Listen("tcp", addr)
	if err != nil {
		return func() error { return nil }, err
	}
	limitedLn := netutil.LimitListener(tcpLn, tcpMaxConns)

	// NotifyStartedFunc dipanggil SETELAH socket berhasil di-bind. Kita pakai
	// untuk memastikan bind sungguh sukses sebelum melapor "aktif" — jangan
	// sampai bind error (mis. IP bukan milik interface) dilaporkan sukses.
	startedc := make(chan struct{}, 2)
	udp := &mdns.Server{
		Addr:              addr,
		Net:               "udp",
		Handler:           h,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		NotifyStartedFunc: func() { startedc <- struct{}{} },
	}
	// TCP server menggunakan Listener yang sudah ada (LimitListener) dan
	// ActivateAndServe agar tidak membuka listener baru.
	tcp := &mdns.Server{
		Listener:          limitedLn,
		Net:               "tcp",
		Handler:           h,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		NotifyStartedFunc: func() { startedc <- struct{}{} },
	}

	errc := make(chan error, 2)
	go func() { errc <- udp.ListenAndServe() }()
	go func() { errc <- tcp.ActivateAndServe() }()

	stop := func() error {
		udp.Shutdown() //nolint:errcheck
		tcp.Shutdown() //nolint:errcheck
		return nil
	}

	// Tunggu kedua listener benar-benar ter-bind, atau error bind pertama,
	// atau timeout singkat (start lambat dianggap OK).
	timeout := time.After(2 * time.Second)
	for started := 0; started < 2; {
		select {
		case err := <-errc:
			stop()
			return func() error { return nil }, err
		case <-startedc:
			started++
		case <-timeout:
			return stop, nil
		}
	}

	return stop, nil
}
