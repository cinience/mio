package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	sqlite "github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// InitDB 初始化数据库连接
func InitDB(config *DatabaseConfig) (*gorm.DB, error) {
	driver, dsn, err := determineDriverAndDSN(config)
	if err != nil {
		return nil, err
	}

	// GORM 配置
	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true, // 使用单数表名
		},
	}

	var db *gorm.DB

	switch driver {
	case "mysql":
		db, err = gorm.Open(mysql.Open(dsn), gormConfig)
	case "postgres":
		db, err = gorm.Open(postgres.Open(dsn), gormConfig)
	case "sqlite":
		if err := ensureSQLiteDir(dsn); err != nil {
			return nil, err
		}
		db, err = gorm.Open(sqlite.Open(dsn), gormConfig)
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", driver)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}

	// 获取通用数据库对象 sql.DB ，然后使用其提供的功能
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	if driver == "sqlite" {
		if config.MaxOpenConns > 0 {
			sqlDB.SetMaxOpenConns(config.MaxOpenConns)
		} else {
			sqlDB.SetMaxOpenConns(1)
		}
		if config.MaxIdleConns > 0 {
			sqlDB.SetMaxIdleConns(config.MaxIdleConns)
		} else {
			sqlDB.SetMaxIdleConns(1)
		}
		sqlDB.SetConnMaxLifetime(0)
	} else {
		// 设置空闲连接池中连接的最大数量
		if config.MaxIdleConns > 0 {
			sqlDB.SetMaxIdleConns(config.MaxIdleConns)
		}

		// 设置打开数据库连接的最大数量
		if config.MaxOpenConns > 0 {
			sqlDB.SetMaxOpenConns(config.MaxOpenConns)
		}

		// 设置连接可复用的最大时间
		sqlDB.SetConnMaxLifetime(time.Hour)
	}

	// 测试数据库连接
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

func ensureSQLiteDir(dsn string) error {
	path := sqliteFilePath(dsn)
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "" || dir == "." || dir == string(filepath.Separator) {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create sqlite directory %s: %w", dir, err)
	}
	return nil
}

func sqliteFilePath(dsn string) string {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "file:") {
		trimmed = strings.TrimPrefix(trimmed, "file:")
	}
	if strings.HasPrefix(trimmed, "//") {
		withoutSlashes := strings.TrimPrefix(trimmed, "//")
		if idx := strings.IndexRune(withoutSlashes, '/'); idx >= 0 {
			trimmed = withoutSlashes[idx+1:]
		} else {
			return ""
		}
	}
	for _, sep := range []string{"?", "#"} {
		if idx := strings.Index(trimmed, sep); idx >= 0 {
			trimmed = trimmed[:idx]
		}
	}
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	if lower == ":memory:" || strings.HasPrefix(lower, "memory") {
		return ""
	}
	return trimmed
}

func determineDriverAndDSN(config *DatabaseConfig) (string, string, error) {
	raw := strings.TrimSpace(config.DSN)
	if raw == "" {
		return "", "", fmt.Errorf("database dsn is empty")
	}

	lowerRaw := strings.ToLower(raw)
	if strings.HasPrefix(lowerRaw, "sqlite::") && !strings.Contains(lowerRaw, "://") {
		dsn := strings.TrimPrefix(raw, "sqlite:")
		if dsn == "" {
			dsn = "file:manager.db?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)"
		}
		return "sqlite", dsn, nil
	}

	if !strings.Contains(raw, "://") {
		return "", "", fmt.Errorf("database dsn must be a URI with scheme")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("invalid database dsn: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)

	switch scheme {
	case "mysql":
		mysqlDSN, err := mysqlDSNFromURI(parsed)
		if err != nil {
			return "", "", err
		}
		return "mysql", mysqlDSN, nil
	case "postgres", "postgresql":
		return "postgres", raw, nil
	case "sqlite", "sqlite3":
		sqliteDSN, err := sqliteDSNFromURI(raw, parsed)
		if err != nil {
			return "", "", err
		}
		return "sqlite", sqliteDSN, nil
	default:
		return "", "", fmt.Errorf("unsupported database scheme: %s", scheme)
	}
}

func mysqlDSNFromURI(u *url.URL) (string, error) {
	var (
		username = ""
		password = ""
	)

	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
	}

	network := u.Query().Get("network")
	if network == "" {
		network = "tcp"
	}

	query := u.Query()
	query.Del("network")

	host := u.Host
	if network == "unix" {
		socketParam := query.Get("socket")
		if socketParam != "" {
			if decoded, err := url.PathUnescape(socketParam); err == nil {
				host = decoded
			} else {
				host = socketParam
			}
			query.Del("socket")
		} else if host != "" {
			if decoded, err := url.PathUnescape(host); err == nil {
				host = decoded
			}
		} else {
			return "", fmt.Errorf("mysql unix network requires socket or host")
		}
	}

	dbName, err := url.PathUnescape(strings.TrimPrefix(u.Path, "/"))
	if err != nil {
		return "", fmt.Errorf("failed to decode mysql database name: %w", err)
	}

	if dbName == "" {
		dbName = query.Get("database")
		query.Del("database")
	}

	var auth string
	if username != "" {
		auth = username
		if password != "" {
			auth += ":" + password
		}
		auth += "@"
	}

	var addr string
	if host != "" {
		addr = fmt.Sprintf("%s(%s)", network, host)
	}

	dsnBuilder := strings.Builder{}
	dsnBuilder.Grow(len(auth) + len(addr) + len(dbName) + len(u.RawQuery) + 16)
	dsnBuilder.WriteString(auth)
	dsnBuilder.WriteString(addr)
	dsnBuilder.WriteString("/")
	dsnBuilder.WriteString(dbName)

	if len(query) > 0 {
		dsnBuilder.WriteString("?")
		dsnBuilder.WriteString(query.Encode())
	}

	return dsnBuilder.String(), nil
}

func sqliteDSNFromURI(raw string, u *url.URL) (string, error) {
	if raw == "" {
		return "file:manager.db?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)", nil
	}

	// Handle opaque forms like sqlite::memory:
	if u.Opaque != "" {
		dsn := u.Opaque
		if u.RawQuery != "" {
			dsn += "?" + u.RawQuery
		}
		if dsn == "" {
			return "file:manager.db?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)", nil
		}
		return dsn, nil
	}

	path := u.Path
	if u.Host != "" {
		if path == "" {
			path = "//" + u.Host
		} else {
			path = "//" + u.Host + path
		}
	}

	if path == "" {
		path = "manager.db"
	}

	decodedPath, err := url.PathUnescape(path)
	if err != nil {
		decodedPath = path
	}

	// Treat leading "/./" or "/../" as relative paths instead of absolute filesystem roots.
	if u.Host == "" {
		switch {
		case strings.HasPrefix(decodedPath, "/./"):
			decodedPath = decodedPath[1:]
		case strings.HasPrefix(decodedPath, "/../"):
			decodedPath = decodedPath[1:]
		case decodedPath == "/.":
			decodedPath = "."
		case decodedPath == "/..":
			decodedPath = ".."
		}
	}

	dsn := decodedPath
	if u.RawQuery != "" {
		dsn += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		dsn += "#" + u.Fragment
	}

	return dsn, nil
}
