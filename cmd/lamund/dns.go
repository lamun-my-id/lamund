package main

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/lamun-my-id/lamund/internal/store"
)

// cmdDNS mengelola zona/record DNS dari CLI — jalur pemulihan saat panel mati
// (temuan audit: menambah A record tanpa panel berarti tulis DB langsung).
// Perubahan record menaikkan serial zona (di store) lalu memberi sinyal reload
// ke data plane agar zona baru langsung disajikan.
func cmdDNS(args []string) {
	if len(args) < 1 {
		fatal("usage: lamund dns <zones|list|add|rm> [flags]")
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("dns "+sub, flag.ExitOnError)
	dbPath := fs.String("db", "/var/lib/lamund/lamund.db", "path database SQLite")
	zone := fs.String("zone", "", "domain zona (mis. example.com)")
	name := fs.String("name", "", "nama record (mis. www; @ untuk apex)")
	rtype := fs.String("type", "A", "tipe: A|AAAA|CNAME|TXT|MX|NS")
	value := fs.String("value", "", "nilai record (mis. 1.2.3.4)")
	ttl := fs.Int64("ttl", 300, "TTL detik")
	priority := fs.Int64("priority", 0, "priority (untuk MX)")
	id := fs.Int64("id", 0, "id record (untuk rm)")
	pidfile := fs.String("reload-pidfile", "/var/lib/lamund/lamund.pid", "pidfile data plane utk sinyal reload")
	fs.Parse(rest)

	st, err := store.Open(*dbPath)
	if err != nil {
		fatal("buka db: %v", err)
	}
	defer st.Close()

	switch sub {
	case "zones":
		zones, err := st.ListDNSZones()
		if err != nil {
			fatal("list zones: %v", err)
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tDOMAIN\tSERIAL")
		for _, z := range zones {
			fmt.Fprintf(tw, "%d\t%s\t%d\n", z.ID, z.Domain, z.Serial)
		}
		tw.Flush()
	case "list":
		z := mustZone(st, *zone)
		recs, err := st.ListDNSRecords(z.ID)
		if err != nil {
			fatal("list records: %v", err)
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tTYPE\tVALUE\tTTL\tPRIO")
		for _, r := range recs {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%d\t%d\n", r.ID, r.Name, r.Type, r.Value, r.TTL, r.Priority)
		}
		tw.Flush()
	case "add":
		if *value == "" {
			fatal("--value wajib untuk add")
		}
		z := mustZone(st, *zone)
		rid, err := st.AddDNSRecord(store.DNSRecord{
			ZoneID: z.ID, Name: *name, Type: *rtype, Value: *value, TTL: *ttl, Priority: *priority,
		})
		if err != nil {
			fatal("add record: %v", err)
		}
		fmt.Printf("record %d ditambahkan ke zona %s\n", rid, z.Domain)
		reloadSignaler(*pidfile)()
	case "rm":
		if *id == 0 {
			fatal("--id wajib untuk rm")
		}
		z := mustZone(st, *zone)
		if err := st.DeleteDNSRecord(z.ID, *id); err != nil {
			fatal("rm record: %v", err)
		}
		fmt.Printf("record %d dihapus dari zona %s\n", *id, z.Domain)
		reloadSignaler(*pidfile)()
	default:
		fatal("subperintah dns tak dikenal: %s", sub)
	}
}

func mustZone(st *store.Store, domain string) *store.DNSZone {
	if domain == "" {
		fatal("--zone wajib")
	}
	z, err := st.GetDNSZone(domain)
	if err != nil {
		fatal("get zone: %v", err)
	}
	if z == nil {
		fatal("zona %s tidak ditemukan", domain)
	}
	return z
}
