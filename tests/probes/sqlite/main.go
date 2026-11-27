// Probe: SQLite (pure Go driver) at Tracer Paper dimensions.
// Writes N sealed turns + ~4 uses per turn, indexed by label; then measures
// a per-turn lookup (uses for 8 labels) at 1K and at N turns. Run twice; compare.
package main

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func lookup(db *sql.DB, maxTurn int) time.Duration {
	t0 := time.Now()
	for k := 0; k < 200; k++ {
		rows, err := db.Query(`SELECT turn, d FROM uses WHERE label IN (?,?,?,?,?,?,?,?) AND turn <= ? ORDER BY turn DESC LIMIT 64`,
			k%5000, (k*7)%5000, (k*13)%5000, (k*17)%5000, (k*19)%5000, (k*23)%5000, (k*29)%5000, (k*31)%5000, maxTurn)
		must(err)
		for rows.Next() {
			var t int
			var d float64
			must(rows.Scan(&t, &d))
		}
		rows.Close()
	}
	return time.Since(t0) / 200
}

func main() {
	path := os.Args[1]
	n := 1000000
	os.Remove(path)
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	must(err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE turns(id INTEGER PRIMARY KEY, spelling BLOB, sha BLOB);
	CREATE TABLE uses(label INTEGER, turn INTEGER, d REAL);
	CREATE INDEX uses_label_turn ON uses(label, turn);`)
	must(err)
	t0 := time.Now()
	tx, _ := db.Begin()
	st, _ := tx.Prepare(`INSERT INTO turns(id, spelling, sha) VALUES(?,?,?)`)
	su, _ := tx.Prepare(`INSERT INTO uses(label, turn, d) VALUES(?,?,?)`)
	for i := 1; i <= n; i++ {
		b := []byte(fmt.Sprintf("turn %d spelled in labels %d %d", i, i%977, i%5000))
		h := sha256.Sum256(b)
		_, err = st.Exec(i, b, h[:])
		must(err)
		for j := 0; j < 4; j++ {
			_, err = su.Exec((i*(j+3))%5000, i, 0.167+0.232*float64(j)/4)
			must(err)
		}
		if i%100000 == 0 {
			must(tx.Commit())
			tx, _ = db.Begin()
			st, _ = tx.Prepare(`INSERT INTO turns(id, spelling, sha) VALUES(?,?,?)`)
			su, _ = tx.Prepare(`INSERT INTO uses(label, turn, d) VALUES(?,?,?)`)
		}
	}
	must(tx.Commit())
	write := time.Since(t0)
	small := lookup(db, 1000)
	large := lookup(db, n)
	t1 := time.Now()
	_, err = db.Exec(`VACUUM INTO '` + path + `.snap'`)
	must(err)
	snap := time.Since(t1)
	fi, _ := os.Stat(path)
	fmt.Printf("turns=%d uses=%d write=%v lookup@1K=%v lookup@%d=%v vacuum_into=%v size=%dMB\n",
		n, 4*n, write.Round(time.Millisecond), small, n, large, snap.Round(time.Millisecond), fi.Size()>>20)
}
