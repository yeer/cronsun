package cronsun

import (
	"github.com/yeer/cronsun/db"
)

var (
	mgoDB *db.Mdb
)

func GetDb() *db.Mdb {
	return mgoDB
}
