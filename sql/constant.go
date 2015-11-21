package sql

import (
	"github.com/tomdionysus/trinity/schema"
)

type Constant struct {
	SQLType schema.SQLType
	Value   string
}

func (me *Constant) ToSQL(wrap bool) string {
	out := ""
	if wrap {
		out += "("
	}
	switch me.SQLType {
	case schema.SQLVarChar:
		out += "\"" + me.Value + "\""
	default:
		out += me.Value
	}
	if wrap {
		out += ")"
	}
	return out
}

func NewConstant(sqlType schema.SQLType, value string) *Constant {
	return &Constant{
		SQLType: sqlType,
		Value:   value,
	}
}
