package migrations

import (
	"embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"

	"manager-server/internal/logger"
	"manager-server/internal/models"
)

//go:embed data
var seedFS embed.FS

type seedTable struct {
	Table string
	File  string
	Model interface{}
}

var seedTables = []seedTable{
	{Table: "sys_dict_type", File: "data/sys_dict_type.csv", Model: &models.SysDictType{}},
	{Table: "sys_dict_data", File: "data/sys_dict_data.csv", Model: &models.SysDictData{}},
	{Table: "sys_params", File: "data/sys_params.csv", Model: &models.SysParams{}},
	{Table: "ai_model_provider", File: "data/ai_model_provider.csv", Model: &models.AIModelProvider{}},
	{Table: "ai_model_config", File: "data/ai_model_config.csv", Model: &models.AIModelConfig{}},
	{Table: "ai_tts_voice", File: "data/ai_tts_voice.csv", Model: &models.AITTSVoice{}},
	{Table: "ai_agent_template", File: "data/ai_agent_template.csv", Model: &models.AgentTemplate{}},
}

var (
	modelSchemaCache sync.Map // map[reflect.Type]*schema.Schema
	parseCache       sync.Map
)

var forceRefreshTables = map[string]struct{}{
	"ai_model_provider": {},
}

type upsertConfig struct {
	Table string
	File  string
	Model interface{}
}

var upsertTables = []upsertConfig{
	{Table: "ai_model_config", File: "data/ai_model_config.csv", Model: &models.AIModelConfig{}},
}

// SeedInitialData populates the database with initial data when empty.
func SeedInitialData(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database handle is nil")
	}

	driver := strings.ToLower(db.Dialector.Name())
	supportedDrivers := map[string]struct{}{
		"mysql":      {},
		"postgres":   {},
		"postgresql": {},
		"sqlite":     {},
	}
	if _, ok := supportedDrivers[driver]; !ok {
		logger.Warnf("SeedInitialData: skipping for unsupported driver %s", driver)
		return nil
	}

	seedDB := db.Session(&gorm.Session{PrepareStmt: true})

	for _, seed := range seedTables {
		hasTable := seedDB.Migrator().HasTable(seed.Table)
		if !hasTable {
			logger.Warnf("SeedInitialData: table %s not found, skipping", seed.Table)
			continue
		}

		empty, err := isTableEmpty(seedDB, seed.Table)
		if err != nil {
			return fmt.Errorf("check table %s: %w", seed.Table, err)
		}
		if empty {
			logger.Infof("SeedInitialData: seeding table %s from %s", seed.Table, seed.File)
			if err := seedTableData(seedDB, seed); err != nil {
				return fmt.Errorf("seed table %s: %w", seed.Table, err)
			}
			continue
		}

		if _, ok := forceRefreshTables[seed.Table]; ok {
			logger.Infof("SeedInitialData: refreshing table %s from %s", seed.Table, seed.File)
			if err := seedDB.Session(&gorm.Session{AllowGlobalUpdate: true}).Table(seed.Table).Delete(nil).Error; err != nil {
				return fmt.Errorf("clear table %s: %w", seed.Table, err)
			}
			if err := seedTableData(seedDB, seed); err != nil {
				return fmt.Errorf("refresh table %s: %w", seed.Table, err)
			}
		}
	}

	for _, cfg := range upsertTables {
		if err := upsertTableData(seedDB, cfg); err != nil {
			return fmt.Errorf("upsert table %s: %w", cfg.Table, err)
		}
	}

	return nil
}

func isTableEmpty(db *gorm.DB, table string) (bool, error) {
	var count int64
	if err := db.Table(table).Count(&count).Error; err != nil {
		return false, err
	}
	return count == 0, nil
}

func seedTableData(db *gorm.DB, seed seedTable) error {
	schemaDef, err := getSeedSchema(db, seed.Model)
	if err != nil {
		return fmt.Errorf("parse schema: %w", err)
	}

	f, err := seedFS.Open(seed.File)
	if err != nil {
		return fmt.Errorf("open seed file: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.TrimLeadingSpace = false

	header, err := r.Read()
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read header: %w", err)
	}

	fieldCache := make(map[string]*schema.Field, len(header))
	var records []map[string]interface{}

	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read record: %w", err)
		}
		if len(row) != len(header) {
			return fmt.Errorf("column count mismatch for table %s: got %d, want %d", seed.Table, len(row), len(header))
		}

		record := make(map[string]interface{}, len(header))
		for i, raw := range row {
			columnName := header[i]
			field := fieldCache[columnName]
			if field == nil && schemaDef != nil {
				field = lookupField(schemaDef, columnName)
				fieldCache[columnName] = field
			}

			parsed, err := convertSeedValue(raw, field)
			if err != nil {
				return fmt.Errorf("parse value for column %s: %w", columnName, err)
			}
			record[columnName] = parsed
		}
		records = append(records, record)
	}

	if len(records) == 0 {
		return nil
	}

	if err := db.Session(&gorm.Session{CreateBatchSize: 100}).Table(seed.Table).Create(&records).Error; err != nil {
		return fmt.Errorf("insert records: %w", err)
	}

	return nil
}

func upsertTableData(db *gorm.DB, cfg upsertConfig) error {
	schemaDef, err := getSeedSchema(db, cfg.Model)
	if err != nil {
		return fmt.Errorf("parse schema: %w", err)
	}

	f, err := seedFS.Open(cfg.File)
	if err != nil {
		return fmt.Errorf("open seed file: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.TrimLeadingSpace = false

	header, err := r.Read()
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read header: %w", err)
	}

	fieldCache := make(map[string]*schema.Field, len(header))

	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read record: %w", err)
		}
		if len(row) != len(header) {
			return fmt.Errorf("column count mismatch for table %s: got %d, want %d", cfg.Table, len(row), len(header))
		}

		record := make(map[string]interface{}, len(header))
		for i, raw := range row {
			columnName := header[i]
			field := fieldCache[columnName]
			if field == nil && schemaDef != nil {
				field = lookupField(schemaDef, columnName)
				fieldCache[columnName] = field
			}

			parsed, err := convertSeedValue(raw, field)
			if err != nil {
				return fmt.Errorf("parse value for column %s: %w", columnName, err)
			}
			record[columnName] = parsed
		}

		if err := upsertRecord(db, cfg.Table, record); err != nil {
			return err
		}
	}

	return nil
}

func upsertRecord(db *gorm.DB, table string, record map[string]interface{}) error {
	primary, ok := record["id"]
	if !ok || primary == nil {
		return nil
	}

	if err := db.Table(table).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(record).Error; err != nil {
		return fmt.Errorf("insert record: %w", err)
	}

	return nil
}

func getSeedSchema(db *gorm.DB, model interface{}) (*schema.Schema, error) {
	if model == nil {
		return nil, nil
	}

	modelType := reflect.TypeOf(model)
	if cached, ok := modelSchemaCache.Load(modelType); ok {
		return cached.(*schema.Schema), nil
	}

	namer := db.Config.NamingStrategy
	if namer == nil {
		namer = schema.NamingStrategy{}
	}

	s, err := schema.Parse(model, &parseCache, namer)
	if err != nil {
		return nil, err
	}
	modelSchemaCache.Store(modelType, s)
	return s, nil
}

func lookupField(s *schema.Schema, column string) *schema.Field {
	if s == nil {
		return nil
	}
	if field, ok := s.FieldsByDBName[column]; ok {
		return field
	}
	lower := strings.ToLower(column)
	for name, field := range s.FieldsByDBName {
		if strings.ToLower(name) == lower {
			return field
		}
	}
	return nil
}

func convertSeedValue(value string, field *schema.Field) (interface{}, error) {
	if value == "" {
		return nil, nil
	}

	if field == nil {
		return value, nil
	}

	fieldType := field.FieldType
	if fieldType.Kind() == reflect.Ptr {
		fieldType = fieldType.Elem()
	}

	switch fieldType.Kind() {
	case reflect.String:
		return value, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, err
		}
		return convertInt(v, fieldType.Kind()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return nil, err
		}
		return convertUint(v, fieldType.Kind()), nil
	case reflect.Float32, reflect.Float64:
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, err
		}
		if fieldType.Kind() == reflect.Float32 {
			return float32(v), nil
		}
		return v, nil
	case reflect.Bool:
		switch strings.ToLower(value) {
		case "1", "true", "t", "yes", "y":
			return true, nil
		case "0", "false", "f", "no", "n":
			return false, nil
		default:
			return nil, fmt.Errorf("invalid bool value %q", value)
		}
	case reflect.Struct:
		if fieldType.PkgPath() == "time" && fieldType.Name() == "Time" {
			return parseTimeValue(value)
		}
	case reflect.Slice:
		if fieldType.Elem().Kind() == reflect.Uint8 {
			// treat as []byte/json
			return datatypes.JSON([]byte(value)), nil
		}
	case reflect.Map:
		if fieldType == reflect.TypeOf(models.JSONMap{}) {
			var parsed models.JSONMap
			if err := json.Unmarshal([]byte(value), &parsed); err != nil {
				return nil, err
			}
			return parsed, nil
		}
		parsed := make(map[string]interface{})
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return nil, err
		}
		return parsed, nil
	}

	// Special-case custom types
	if fieldType == reflect.TypeOf(datatypes.JSON{}) {
		return datatypes.JSON([]byte(value)), nil
	}

	return value, nil
}

func convertInt(v int64, kind reflect.Kind) interface{} {
	switch kind {
	case reflect.Int8:
		return int8(v)
	case reflect.Int16:
		return int16(v)
	case reflect.Int32:
		return int32(v)
	case reflect.Int64:
		return v
	default:
		return int(v)
	}
}

func convertUint(v uint64, kind reflect.Kind) interface{} {
	switch kind {
	case reflect.Uint8:
		return uint8(v)
	case reflect.Uint16:
		return uint16(v)
	case reflect.Uint32:
		return uint32(v)
	case reflect.Uint64:
		return v
	default:
		return uint(v)
	}
}

func parseTimeValue(value string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if ts, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return ts, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time format %q", value)
}

func parseValue(value string, columnType gorm.ColumnType) (interface{}, error) {
	if value == "" {
		return nil, nil
	}

	scanType := columnType.ScanType()
	if scanType != nil && scanType.Kind() == reflect.Ptr {
		scanType = scanType.Elem()
	}

	switch {
	case scanType == nil:
		return value, nil
	case scanType.Kind() >= reflect.Int && scanType.Kind() <= reflect.Int64:
		v, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, err
		}
		switch scanType.Kind() {
		case reflect.Int:
			return int(v), nil
		case reflect.Int8:
			return int8(v), nil
		case reflect.Int16:
			return int16(v), nil
		case reflect.Int32:
			return int32(v), nil
		default:
			return v, nil
		}
	case scanType.Kind() >= reflect.Uint && scanType.Kind() <= reflect.Uint64:
		v, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return nil, err
		}
		switch scanType.Kind() {
		case reflect.Uint:
			return uint(v), nil
		case reflect.Uint8:
			return uint8(v), nil
		case reflect.Uint16:
			return uint16(v), nil
		case reflect.Uint32:
			return uint32(v), nil
		default:
			return v, nil
		}
	case scanType.Kind() == reflect.Float32 || scanType.Kind() == reflect.Float64:
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, err
		}
		if scanType.Kind() == reflect.Float32 {
			return float32(v), nil
		}
		return v, nil
	case scanType.Kind() == reflect.Bool:
		switch strings.ToLower(value) {
		case "1", "true", "t", "y", "yes":
			return true, nil
		case "0", "false", "f", "n", "no":
			return false, nil
		default:
			return nil, fmt.Errorf("invalid boolean value %s", value)
		}
	case scanType.Kind() == reflect.Struct && scanType == reflect.TypeOf(time.Time{}):
		layouts := []string{
			time.RFC3339Nano,
			"2006-01-02 15:04:05.999999",
			"2006-01-02 15:04:05.000",
			"2006-01-02 15:04:05",
		}
		var parsed time.Time
		var err error
		for _, layout := range layouts {
			parsed, err = time.ParseInLocation(layout, value, time.Local)
			if err == nil {
				return parsed, nil
			}
		}
		return nil, fmt.Errorf("invalid time value %s", value)
	case scanType.Kind() == reflect.Slice && scanType.Elem().Kind() == reflect.Uint8:
		return value, nil
	default:
		return value, nil
	}
}
