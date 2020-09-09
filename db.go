package yiigo

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jinzhu/gorm"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pelletier/go-toml"
	"gitlab.meipian.cn/golib/yiigo/v2"
	"go.uber.org/zap"
)

type DBDriver string

const (
	MySQL    DBDriver = "mysql"
	Postgres DBDriver = "postgres"
	SQLite   DBDriver = "sqlite3"
)

var (
	defaultDB  *sqlx.DB
	dbmap      sync.Map
	defaultOrm *gorm.DB
	ormap      sync.Map
)

type dbConfig struct {
	Driver          string `toml:"driver"`
	Dsn             string `toml:"dsn"`
	MaxOpenConns    int    `toml:"max_open_conns"`
	MaxIdleConns    int    `toml:"max_idle_conns"`
	ConnMaxLifetime int    `toml:"conn_max_lifetime"`
}

func dbDial(cfg *dbConfig, debug bool) (*gorm.DB, error) {
	if !InStrings(cfg.Driver, string(MySQL), string(Postgres), string(SQLite)) {
		return nil, fmt.Errorf("yiigo: unknown db driver %s, expects mysql, postgres, sqlite3", cfg.Driver)
	}

	orm, err := gorm.Open(cfg.Driver, cfg.Dsn)

	if err != nil {
		return nil, err
	}

	if debug {
		orm.LogMode(true)
	}

	orm.DB().SetMaxOpenConns(cfg.MaxOpenConns)
	orm.DB().SetMaxIdleConns(cfg.MaxIdleConns)
	orm.DB().SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)

	return orm, nil
}

func initDB(debug bool) {
	tree, ok := env.get("db").(*toml.Tree)

	if !ok {
		return
	}

	keys := tree.Keys()

	if len(keys) == 0 {
		return
	}

	for _, v := range keys {
		node, ok := tree.Get(v).(*toml.Tree)

		if !ok {
			continue
		}

		cfg := new(dbConfig)

		if err := node.Unmarshal(cfg); err != nil {
			logger.Panic("yiigo: db init error", zap.String("name", v), zap.Error(err))
		}

		orm, err := dbDial(cfg, debug)

		if err != nil {
			logger.Panic("yiigo: db init error", zap.String("name", v), zap.Error(err))
		}

		db := sqlx.NewDb(orm.DB(), cfg.Driver)

		if v == AsDefault {
			defaultDB = db
			defaultOrm = orm
		}

		dbmap.Store(v, db)
		ormap.Store(v, orm)

		logger.Info(fmt.Sprintf("yiigo: db.%s is OK.", v))
	}
}

// DB returns a db.
func DB(name ...string) *sqlx.DB {
	if len(name) == 0 {
		if defaultDB == nil {
			logger.Panic(fmt.Sprintf("yiigo: unknown db.%s (forgotten configure?)", AsDefault))
		}

		return defaultDB
	}

	v, ok := dbmap.Load(name[0])

	if !ok {
		logger.Panic(fmt.Sprintf("yiigo: unknown db.%s (forgotten configure?)", name[0]))
	}

	return v.(*sqlx.DB)
}

// Orm returns an orm's db.
func Orm(name ...string) *gorm.DB {
	if len(name) == 0 || name[0] == AsDefault {
		if defaultOrm == nil {
			logger.Panic(fmt.Sprintf("yiigo: unknown db.%s (forgotten configure?)", AsDefault))
		}

		return defaultOrm
	}

	v, ok := ormap.Load(name[0])

	if !ok {
		logger.Panic(fmt.Sprintf("yiigo: unknown db.%s (forgotten configure?)", name[0]))
	}

	return v.(*gorm.DB)
}

type SQLClause struct {
	query string
	args  []interface{}
}

// Clause returns sql clause, eg: yiigo.Clause("price * ? + ?", 2, 100).
func Clause(query string, args ...interface{}) *SQLClause {
	return &SQLClause{
		query: query,
		args:  args,
	}
}

// SQLBuilder build SQL statement
type SQLBuilder struct {
	driver   DBDriver
	table    string
	columns  []string
	distinct []string
	where    *SQLClause
	joins    []string
	group    string
	having   *SQLClause
	order    string
	offset   int
	limit    int
	values   []string
	sets     []string
	binds    []interface{}
	queryLen int
	bindsLen int
}

// Table add query table
func (b *SQLBuilder) Table(table string) *SQLBuilder {
	b.table = table
	b.queryLen += 2

	return b
}

// Select add query columns
func (b *SQLBuilder) Select(columns ...string) *SQLBuilder {
	b.columns = columns
	b.queryLen += 2

	return b
}

// Distinct add distinct clause
func (b *SQLBuilder) Distinct(columns ...string) *SQLBuilder {
	b.distinct = columns
	b.queryLen += 2

	return b
}

// Distinct add inner join clause
func (b *SQLBuilder) InnerJoin(table, on string) *SQLBuilder {
	b.joins = append(b.joins, "INNER", "JOIN", table, "ON", on)
	b.queryLen += 5

	return b
}

// Distinct add left join clause
func (b *SQLBuilder) LeftJoin(table, on string) *SQLBuilder {
	b.joins = append(b.joins, "LEFT", "JOIN", table, "ON", on)
	b.queryLen += 5

	return b
}

// Distinct add right join clause
func (b *SQLBuilder) RightJoin(table, on string) *SQLBuilder {
	b.joins = append(b.joins, "RIGHT", "JOIN", table, "ON", on)
	b.queryLen += 5

	return b
}

// Distinct add full join clause
func (b *SQLBuilder) FullJoin(table, on string) *SQLBuilder {
	b.joins = append(b.joins, "FULL", "JOIN", table, "ON", on)
	b.queryLen += 5

	return b
}

// Distinct add where clause
func (b *SQLBuilder) Where(query string, args ...interface{}) *SQLBuilder {
	b.where = Clause(query, args...)

	b.queryLen += 2
	b.bindsLen += len(args)

	return b
}

// Distinct add group clause
func (b *SQLBuilder) Group(column string) *SQLBuilder {
	b.group = column
	b.queryLen += 2

	return b
}

// Distinct add having clause
func (b *SQLBuilder) Having(query string, args ...interface{}) *SQLBuilder {
	b.having = Clause(query, args...)

	b.queryLen += 2
	b.bindsLen += len(args)

	return b
}

// Order add order clause
func (b *SQLBuilder) Order(query string) *SQLBuilder {
	b.order = query
	b.queryLen += 2

	return b
}

// Distinct add offset clause
func (b *SQLBuilder) Offset(offset int) *SQLBuilder {
	b.offset = offset
	b.queryLen += 2

	return b
}

// Distinct add limit clause
func (b *SQLBuilder) Limit(limit int) *SQLBuilder {
	b.limit = limit
	b.queryLen += 2

	return b
}

// ToToQuery returns query clause and binds.
func (b *SQLBuilder) ToQuery() (string, []interface{}) {
	query := make([]string, 0, b.queryLen+2)
	b.binds = make([]interface{}, 0, b.bindsLen)

	query = append(query, "SELECT")

	if len(b.distinct) != 0 {
		query = append(query, "DISTINCT", strings.Join(b.distinct, ", "))
	} else if len(b.columns) != 0 {
		query = append(query, strings.Join(b.columns, ", "))
	} else {
		query = append(query, "*")
	}

	query = append(query, "FROM", b.table)

	if len(b.joins) != 0 {
		query = append(query, b.joins...)
	}

	if b.where != nil {
		query = append(query, "WHERE", b.where.query)
		b.binds = append(b.binds, b.where.args...)
	}

	if b.group != "" {
		query = append(query, "GROUP BY", b.group)
	}

	if b.having != nil {
		query = append(query, "HAVING", b.having.query)
		b.binds = append(b.binds, b.having.args...)
	}

	if b.order != "" {
		query = append(query, "ORDER BY", b.order)
	}

	if b.offset != 0 {
		query = append(query, "OFFSET", strconv.Itoa(b.offset))
	}

	if b.limit != 0 {
		query = append(query, "LIMIT", strconv.Itoa(b.limit))
	}

	return sqlx.Rebind(sqlx.BindType(string(b.driver)), strings.Join(query, " ")), b.binds
}

// ToInsert returns insert clause and binds.
// data expects `struct`, `*struct`, `yiigo.X`.
func (b *SQLBuilder) ToInsert(data interface{}) (string, []interface{}) {
	v := reflect.Indirect(reflect.ValueOf(data))

	switch v.Kind() {
	case reflect.Map:
		x, ok := data.(yiigo.X)

		if !ok {
			Logger().Error("invalid data type for insert, expects struct, *struct, yiigo.X")

			return "", nil
		}

		b.insertWithMap(x)
	case reflect.Struct:
		b.insertWithStruct(v)
	default:
		Logger().Error("invalid data type for insert, expects struct, *struct, yiigo.X")

		return "", nil
	}

	query := make([]string, 0, 12)

	query = append(query, "INSERT", "INTO", b.table, "(", strings.Join(b.columns, ", "), ")", "VALUES", "(", strings.Join(b.values, ", "), ")")

	if b.driver == Postgres {
		query = append(query, "RETURNING", "id")
	}

	return sqlx.Rebind(sqlx.BindType(string(b.driver)), strings.Join(query, " ")), b.binds
}

func (b *SQLBuilder) insertWithMap(data yiigo.X) {
	fieldNum := len(data)

	b.columns = make([]string, 0, fieldNum)
	b.values = make([]string, 0, fieldNum)
	b.binds = make([]interface{}, 0, fieldNum)

	for k, v := range data {
		b.columns = append(b.columns, k)
		b.values = append(b.values, "?")
		b.binds = append(b.binds, v)
	}
}

func (b *SQLBuilder) insertWithStruct(v reflect.Value) {
	fieldNum := v.NumField()

	b.columns = make([]string, 0, fieldNum)
	b.values = make([]string, 0, fieldNum)
	b.binds = make([]interface{}, 0, fieldNum)

	t := v.Type()

	for i := 0; i < fieldNum; i++ {
		column := t.Field(i).Tag.Get("db")

		if column == "-" {
			continue
		}

		if column == "" {
			column = t.Field(i).Name
		}

		b.columns = append(b.columns, column)
		b.values = append(b.values, "?")
		b.binds = append(b.binds, v.Field(i).Interface())
	}
}

// ToBatchInsert returns batch insert clause and binds.
// data expects `[]struct`, `[]*struct`, `[]yiigo.X`.
func (b *SQLBuilder) ToBatchInsert(data interface{}) (string, []interface{}) {
	v := reflect.Indirect(reflect.ValueOf(data))

	switch v.Kind() {
	case reflect.Slice:
		if v.Len() == 0 {
			return "", nil
		}

		e := v.Type().Elem()

		switch e.Kind() {
		case reflect.Map:
			x, ok := data.([]yiigo.X)

			if !ok {
				Logger().Error("invalid data type for batch insert, expects []struct, []*struct, []yiigo.X")

				return "", nil
			}

			b.batchInsertWithMap(x)
		case reflect.Struct:
			b.batchInsertWithStruct(v)
		case reflect.Ptr:
			if e.Elem().Kind() != reflect.Struct {
				Logger().Error("invalid data type for batch insert, expects []struct, []*struct, []yiigo.X")

				return "", nil
			}

			b.batchInsertWithStruct(v)
		default:
			Logger().Error("invalid data type for batch insert, expects []struct, []*struct, []yiigo.X")

			return "", nil
		}
	default:
		Logger().Error("invalid data type for batch insert, expects []struct, []*struct, []yiigo.X")

		return "", nil
	}

	query := []string{"INSERT", "INTO", b.table, "(", strings.Join(b.columns, ", "), ")", "VALUES", strings.Join(b.values, ", ")}

	return sqlx.Rebind(sqlx.BindType(string(b.driver)), strings.Join(query, " ")), b.binds
}

func (b *SQLBuilder) batchInsertWithMap(data []yiigo.X) {
	dataLen := len(data)
	fieldNum := len(data[0])

	b.columns = make([]string, 0, fieldNum)
	b.values = make([]string, 0, dataLen)
	b.binds = make([]interface{}, 0, fieldNum*dataLen)

	for k := range data[0] {
		b.columns = append(b.columns, k)
	}

	for _, x := range data {
		phrs := make([]string, 0, fieldNum)

		for _, v := range b.columns {
			phrs = append(phrs, "?")
			b.binds = append(b.binds, x[v])
		}

		b.values = append(b.values, fmt.Sprintf("( %s )", strings.Join(phrs, ", ")))
	}
}

func (b *SQLBuilder) batchInsertWithStruct(v reflect.Value) {
	first := reflect.Indirect(v.Index(0))

	dataLen := v.Len()
	fieldNum := first.NumField()

	b.columns = make([]string, 0, fieldNum)
	b.values = make([]string, 0, dataLen)
	b.binds = make([]interface{}, 0, fieldNum*dataLen)

	t := first.Type()

	for i := 0; i < dataLen; i++ {
		phrs := make([]string, 0, fieldNum)

		for j := 0; j < fieldNum; j++ {
			column := t.Field(j).Tag.Get("db")

			if column == "-" {
				continue
			}

			if i == 0 {
				if column == "" {
					column = t.Field(j).Name
				}

				b.columns = append(b.columns, column)
			}

			phrs = append(phrs, "?")
			b.binds = append(b.binds, reflect.Indirect(v.Index(i)).Field(j).Interface())
		}

		b.values = append(b.values, fmt.Sprintf("( %s )", strings.Join(phrs, ", ")))
	}
}

// ToUpdate returns update clause and binds.
// data expects `struct`, `*struct`, `yiigo.X`.
func (b *SQLBuilder) ToUpdate(data interface{}) (string, []interface{}) {
	v := reflect.Indirect(reflect.ValueOf(data))

	switch v.Kind() {
	case reflect.Map:
		x, ok := data.(yiigo.X)

		if !ok {
			Logger().Error("invalid data type for update, expects struct, *struct, yiigo.X")

			return "", nil
		}

		b.updateWithMap(x)
	case reflect.Struct:
		b.updateWithStruct(v)
	default:
		Logger().Error("invalid data type for update, expects struct, *struct, yiigo.X")

		return "", nil
	}

	query := make([]string, 0)

	query = append(query, "UPDATE", b.table, "SET", strings.Join(b.sets, ", "))

	if b.where != nil {
		query = append(query, "WHERE", b.where.query)
		b.binds = append(b.binds, b.where.args...)
	}

	return sqlx.Rebind(sqlx.BindType(string(b.driver)), strings.Join(query, " ")), b.binds
}

func (b *SQLBuilder) updateWithMap(data yiigo.X) {
	fieldNum := len(data)

	b.sets = make([]string, 0, fieldNum)
	b.binds = make([]interface{}, 0, fieldNum+b.bindsLen)

	for k, v := range data {
		if clause, ok := v.(*SQLClause); ok {
			b.sets = append(b.sets, fmt.Sprintf("%s = %s", k, clause.query))
			b.binds = append(b.binds, clause.args...)

			continue
		}

		b.sets = append(b.sets, fmt.Sprintf("%s = ?", k))
		b.binds = append(b.binds, v)
	}
}

func (b *SQLBuilder) updateWithStruct(v reflect.Value) {
	fieldNum := v.NumField()

	b.sets = make([]string, 0, fieldNum)
	b.binds = make([]interface{}, 0, fieldNum+b.bindsLen)

	t := v.Type()

	for i := 0; i < fieldNum; i++ {
		column := t.Field(i).Tag.Get("db")

		if column == "-" {
			continue
		}

		if column == "" {
			column = t.Field(i).Name
		}

		b.sets = append(b.sets, fmt.Sprintf("%s = ?", column))
		b.binds = append(b.binds, v.Field(i).Interface())
	}
}

// ToDelete returns delete clause and binds.
func (b *SQLBuilder) ToDelete() (string, []interface{}) {
	query := make([]string, 0, b.queryLen)
	binds := make([]interface{}, 0, b.bindsLen)

	query = append(query, "DELETE", "FROM", b.table)

	if b.where != nil {
		query = append(query, "WHERE", b.where.query)
		binds = append(binds, b.where.args...)
	}

	return sqlx.Rebind(sqlx.BindType(string(b.driver)), strings.Join(query, " ")), binds
}

func NewSQLBuilder(driver DBDriver) *SQLBuilder {
	return &SQLBuilder{driver: driver}
}
