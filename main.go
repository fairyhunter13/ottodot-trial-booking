package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/fairyhunter13/ottodot-trial-booking/internal/store"
	"github.com/fairyhunter13/ottodot-trial-booking/internal/web"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	path := flag.String("db", "data.db", "sqlite file")
	keep := flag.Bool("keep", false, "keep the existing database instead of rebuilding the demo seed")
	flag.Parse()

	if !*keep {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			os.Remove(*path + suffix)
		}
	}
	db, err := store.Open(*path, !*keep)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	log.Printf("listening on http://localhost%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, web.NewServer(db).Routes()))
}
