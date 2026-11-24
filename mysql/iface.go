package mysql

import "github.com/go-mist/mist"

var (
	_ mist.DB = (*DB)(nil)
	_ mist.Tx = (*Tx)(nil)
)
