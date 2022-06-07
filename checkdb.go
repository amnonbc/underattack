package main

import (
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
)

func checkDb(conf Config) error {
	db, err := sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(127.0.0.1:3306)/%s",
		conf.DbUser, conf.DbPassword, conf.DbName))
	if err != nil {
		return err
	}
	return db.Close()
}
