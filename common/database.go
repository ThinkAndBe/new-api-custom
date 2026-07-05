package common

type DatabaseType string

const (
	DatabaseTypeMySQL      DatabaseType = "mysql"
	DatabaseTypeSQLite     DatabaseType = "sqlite"
	DatabaseTypePostgreSQL DatabaseType = "postgres"
	DatabaseTypeClickHouse DatabaseType = "clickhouse"
)

var mainDatabaseType = DatabaseTypeSQLite
var logDatabaseType = DatabaseTypeSQLite

// 兼容 fork 旧变量
var UsingSQLite = false
var UsingPostgreSQL = false
var LogSqlType = DatabaseTypeSQLite
var UsingMySQL = false
var UsingClickHouse = false

func MainDatabaseType() DatabaseType { return mainDatabaseType }
func LogDatabaseType() DatabaseType { return logDatabaseType }
func SetMainDatabaseType(t DatabaseType) { mainDatabaseType = t }
func SetLogDatabaseType(t DatabaseType) { logDatabaseType = t }
func SetDatabaseTypes(mainType, logType DatabaseType) { mainDatabaseType = mainType; logDatabaseType = logType }
func UsingMainDatabase(t DatabaseType) bool { return mainDatabaseType == t }
func UsingLogDatabase(t DatabaseType) bool { return logDatabaseType == t }

var SQLitePath = "one-api.db?_busy_timeout=30000"
