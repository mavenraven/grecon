package db

import (
	"database/sql"
)

func OpenTestDB() *sql.DB {
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		panic(err)
	}
	if err := migrate(d); err != nil {
		panic(err)
	}
	return d
}
