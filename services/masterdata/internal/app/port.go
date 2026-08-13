package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

type Port struct {
	ID                                                    int64
	UNLOCODE, NameZH, NameEN, CountryCode, City, Timezone string
	Aliases                                               []string
	Status, Remark, PortType, AdminArea                   string
	Latitude, Longitude                                   float64
	HasCoordinates, IsFavorite                            bool
	Version                                               int32
}

type PortInput struct {
	Port
	OperatorID   int64
	OperatorName string
}

type PortCountryCount struct {
	CountryCode string
	PortCount   int64
}

type PortImportRow struct {
	RowNumber            int32
	ProfileFieldsPresent bool
	PortInput
}

type PortImportIssue struct {
	RowNumber int32
	Code      string
	Message   string
}

type PortImportResult struct {
	CreateCount, UpdateCount, SkipCount int32
	Issues                              []PortImportIssue
}

// validatePortInput 在数据库写入前统一验证国际代码和 IANA 时区，避免无效港口进入船期选择器。
func validatePortInput(in *PortInput) error {
	in.UNLOCODE = strings.ToUpper(strings.TrimSpace(in.UNLOCODE))
	in.CountryCode = strings.ToUpper(strings.TrimSpace(in.CountryCode))
	in.NameZH, in.NameEN = strings.TrimSpace(in.NameZH), strings.TrimSpace(in.NameEN)
	in.City, in.Timezone, in.Remark = strings.TrimSpace(in.City), strings.TrimSpace(in.Timezone), strings.TrimSpace(in.Remark)
	in.AdminArea, in.PortType = strings.TrimSpace(in.AdminArea), strings.ToUpper(strings.TrimSpace(in.PortType))
	if in.PortType == "" {
		in.PortType = "SEAPORT"
	}
	if len(in.UNLOCODE) != 5 || len(in.CountryCode) != 2 || !strings.HasPrefix(in.UNLOCODE, in.CountryCode) {
		return apierr.Invalid("MD_PORT_UNLOCODE_INVALID", "UN/LOCODE 必须是国家代码加三位地点代码，例如 CNSHA")
	}
	if in.NameZH == "" && in.NameEN == "" {
		return apierr.Invalid("MD_PORT_NAME_REQUIRED", "港口中文名称和英文名称至少填写一项")
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		return apierr.Invalid("MD_PORT_TIMEZONE_INVALID", "请输入有效的 IANA 时区，例如 Asia/Shanghai")
	}
	// 对已维护时区规则的国家做联动校验；未覆盖国家仍接受合法 IANA 时区，避免阻断全球港口录入。
	countryTimezones := map[string]map[string]bool{
		"CN": {"Asia/Shanghai": true}, "HK": {"Asia/Hong_Kong": true}, "TW": {"Asia/Taipei": true},
		"SG": {"Asia/Singapore": true}, "JP": {"Asia/Tokyo": true}, "KR": {"Asia/Seoul": true},
		"GB": {"Europe/London": true}, "DE": {"Europe/Berlin": true}, "NL": {"Europe/Amsterdam": true},
		"BE": {"Europe/Brussels": true}, "FR": {"Europe/Paris": true}, "IT": {"Europe/Rome": true},
	}
	if allowed, knownCountry := countryTimezones[in.CountryCode]; knownCountry && !allowed[in.Timezone] {
		return apierr.Invalid("MD_PORT_COUNTRY_TIMEZONE_MISMATCH", "港口时区与所选国家/地区不匹配")
	}
	validTypes := map[string]bool{"SEAPORT": true, "RIVER_PORT": true, "DRY_PORT": true, "AIRPORT": true, "OTHER": true}
	if !validTypes[in.PortType] {
		return apierr.Invalid("MD_PORT_TYPE_INVALID", "请选择有效的港口类型")
	}
	if in.HasCoordinates && (in.Latitude < -90 || in.Latitude > 90 || in.Longitude < -180 || in.Longitude > 180) {
		return apierr.Invalid("MD_PORT_COORDINATES_INVALID", "港口经纬度超出有效范围")
	}
	seen := map[string]bool{}
	clean := make([]string, 0, len(in.Aliases))
	for _, alias := range in.Aliases {
		alias = strings.TrimSpace(alias)
		if alias != "" && !seen[strings.ToLower(alias)] {
			seen[strings.ToLower(alias)] = true
			clean = append(clean, alias)
		}
	}
	in.Aliases = clean
	return nil
}

func scanPort(row pgx.Row) (Port, error) {
	var p Port
	err := row.Scan(&p.ID, &p.UNLOCODE, &p.NameZH, &p.NameEN, &p.CountryCode, &p.City, &p.Timezone, &p.Aliases, &p.Status, &p.Remark, &p.Version, &p.PortType, &p.AdminArea, &p.Latitude, &p.Longitude, &p.HasCoordinates, &p.IsFavorite)
	return p, err
}

func (s *Service) GetPort(ctx context.Context, tenantID, id int64) (Port, error) {
	p, err := scanPort(s.pool.QueryRow(ctx, `SELECT id, unlocode, name_zh, name_en, country_code, city, timezone, aliases, status, remark, version, port_type, admin_area, latitude, longitude, has_coordinates, is_favorite FROM ports WHERE tenant_id=$1 AND id=$2`, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Port{}, apierr.NotFound("MD_PORT_NOT_FOUND", "港口不存在")
	}
	return p, err
}

// ListPorts 使用后端分页和国家筛选，避免导入全球港口后一次加载全部数据。
func (s *Service) ListPorts(ctx context.Context, tenantID int64, keyword, countryCode, status string, page, size int32) ([]Port, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	keyword, countryCode, status = strings.TrimSpace(keyword), strings.ToUpper(strings.TrimSpace(countryCode)), strings.ToUpper(strings.TrimSpace(status))
	if status == "" {
		status = "ACTIVE"
	}
	pattern := "%" + keyword + "%"
	var total int64
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM ports WHERE tenant_id=$1 AND ($2='ALL' OR status=$2) AND ($3='' OR country_code=$3) AND ($4='' OR unlocode ILIKE $5 OR name_zh ILIKE $5 OR name_en ILIKE $5 OR city ILIKE $5 OR admin_area ILIKE $5 OR array_to_string(aliases,' ') ILIKE $5)`, tenantID, status, countryCode, keyword, pattern).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id, unlocode, name_zh, name_en, country_code, city, timezone, aliases, status, remark, version, port_type, admin_area, latitude, longitude, has_coordinates, is_favorite FROM ports WHERE tenant_id=$1 AND ($2='ALL' OR status=$2) AND ($3='' OR country_code=$3) AND ($4='' OR unlocode ILIKE $5 OR name_zh ILIKE $5 OR name_en ILIKE $5 OR city ILIKE $5 OR admin_area ILIKE $5 OR array_to_string(aliases,' ') ILIKE $5) ORDER BY is_favorite DESC, country_code, unlocode LIMIT $6 OFFSET $7`, tenantID, status, countryCode, keyword, pattern, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	ports := []Port{}
	for rows.Next() {
		p, err := scanPort(rows)
		if err != nil {
			return nil, 0, err
		}
		ports = append(ports, p)
	}
	return ports, total, rows.Err()
}

// ListPortCountries 按标准国家代码汇总港口数量，分类栏无需加载全部港口。
func (s *Service) ListPortCountries(ctx context.Context, tenantID int64, status string) ([]PortCountryCount, error) {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status == "" {
		status = "ACTIVE"
	}
	rows, err := s.pool.Query(ctx, `SELECT country_code, count(*) FROM ports WHERE tenant_id=$1 AND ($2='ALL' OR status=$2) GROUP BY country_code ORDER BY country_code`, tenantID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PortCountryCount{}
	for rows.Next() {
		var item PortCountryCount
		if err = rows.Scan(&item.CountryCode, &item.PortCount); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func sameImportedPort(a Port, b PortInput) bool {
	return a.NameZH == b.NameZH && a.NameEN == b.NameEN && a.CountryCode == b.CountryCode && a.City == b.City && a.Timezone == b.Timezone && a.Remark == b.Remark && a.PortType == b.PortType && a.AdminArea == b.AdminArea && a.Latitude == b.Latitude && a.Longitude == b.Longitude && a.HasCoordinates == b.HasCoordinates && a.IsFavorite == b.IsFavorite && strings.Join(a.Aliases, "\x00") == strings.Join(b.Aliases, "\x00")
}

// ImportPorts 先完整预检，再在一个事务内新增或更新；任一错误行都会阻止整批写入。
func (s *Service) ImportPorts(ctx context.Context, tenantID int64, rows []PortImportRow, confirm bool, opID int64, opName string) (PortImportResult, error) {
	result := PortImportResult{Issues: []PortImportIssue{}}
	if len(rows) == 0 || len(rows) > 5000 {
		return result, apierr.Invalid("MD_PORT_IMPORT_SIZE_INVALID", "每次请输入 1 至 5000 条港口资料")
	}
	seen := map[string]int32{}
	for i := range rows {
		rows[i].OperatorID, rows[i].OperatorName = opID, opName
		if rows[i].RowNumber == 0 {
			rows[i].RowNumber = int32(i + 2)
		}
		if err := validatePortInput(&rows[i].PortInput); err != nil {
			result.Issues = append(result.Issues, PortImportIssue{rows[i].RowNumber, "MD_PORT_IMPORT_ROW_INVALID", err.Error()})
			continue
		}
		if first, ok := seen[rows[i].UNLOCODE]; ok {
			result.Issues = append(result.Issues, PortImportIssue{rows[i].RowNumber, "MD_PORT_IMPORT_DUPLICATE", fmt.Sprintf("与第 %d 行港口代码重复", first)})
		} else {
			seen[rows[i].UNLOCODE] = rows[i].RowNumber
		}
	}
	if len(result.Issues) > 0 {
		return result, nil
	}
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		for _, row := range rows {
			existing, err := scanPort(tx.QueryRow(ctx, `SELECT id,unlocode,name_zh,name_en,country_code,city,timezone,aliases,status,remark,version,port_type,admin_area,latitude,longitude,has_coordinates,is_favorite FROM ports WHERE tenant_id=$1 AND unlocode=$2 FOR UPDATE`, tenantID, row.UNLOCODE))
			if errors.Is(err, pgx.ErrNoRows) {
				result.CreateCount++
				if !confirm {
					continue
				}
				created, createErr := scanPort(tx.QueryRow(ctx, `INSERT INTO ports (tenant_id,unlocode,name_zh,name_en,country_code,city,timezone,aliases,remark,port_type,admin_area,latitude,longitude,has_coordinates,is_favorite,created_by,created_by_name,updated_by,updated_by_name) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$16,$17) RETURNING id,unlocode,name_zh,name_en,country_code,city,timezone,aliases,status,remark,version,port_type,admin_area,latitude,longitude,has_coordinates,is_favorite`, tenantID, row.UNLOCODE, row.NameZH, row.NameEN, row.CountryCode, row.City, row.Timezone, row.Aliases, row.Remark, row.PortType, row.AdminArea, row.Latitude, row.Longitude, row.HasCoordinates, row.IsFavorite, opID, opName))
				if createErr != nil {
					return portUniqueConflict(createErr)
				}
				_, err = tx.Exec(ctx, `INSERT INTO port_change_logs (tenant_id,port_id,action,after_data,operator_id,operator_name) VALUES ($1,$2,'IMPORT_CREATE',$3,$4,$5)`, tenantID, created.ID, portJSON(created), opID, opName)
				if err != nil {
					return err
				}
				continue
			}
			if err != nil {
				return err
			}
			// 兼容旧版 CSV：模板没有扩展列时，仅更新旧字段，不清空已维护的港口画像。
			if !row.ProfileFieldsPresent {
				row.PortType, row.AdminArea = existing.PortType, existing.AdminArea
				row.Latitude, row.Longitude = existing.Latitude, existing.Longitude
				row.HasCoordinates, row.IsFavorite = existing.HasCoordinates, existing.IsFavorite
			}
			if sameImportedPort(existing, row.PortInput) {
				result.SkipCount++
				continue
			}
			result.UpdateCount++
			if !confirm {
				continue
			}
			updated, updateErr := scanPort(tx.QueryRow(ctx, `UPDATE ports SET name_zh=$3,name_en=$4,country_code=$5,city=$6,timezone=$7,aliases=$8,remark=$9,port_type=$10,admin_area=$11,latitude=$12,longitude=$13,has_coordinates=$14,is_favorite=$15,version=version+1,updated_at=now(),updated_by=$16,updated_by_name=$17 WHERE tenant_id=$1 AND id=$2 RETURNING id,unlocode,name_zh,name_en,country_code,city,timezone,aliases,status,remark,version,port_type,admin_area,latitude,longitude,has_coordinates,is_favorite`, tenantID, existing.ID, row.NameZH, row.NameEN, row.CountryCode, row.City, row.Timezone, row.Aliases, row.Remark, row.PortType, row.AdminArea, row.Latitude, row.Longitude, row.HasCoordinates, row.IsFavorite, opID, opName))
			if updateErr != nil {
				return updateErr
			}
			_, err = tx.Exec(ctx, `INSERT INTO port_change_logs (tenant_id,port_id,action,before_data,after_data,operator_id,operator_name) VALUES ($1,$2,'IMPORT_UPDATE',$3,$4,$5,$6)`, tenantID, existing.ID, portJSON(existing), portJSON(updated), opID, opName)
			if err != nil {
				return err
			}
		}
		return nil
	})
	return result, err
}

func portJSON(p Port) []byte { b, _ := json.Marshal(p); return b }

func portUniqueConflict(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return apierr.Conflict("MD_PORT_UNLOCODE_EXISTS", "该 UN/LOCODE 已存在")
	}
	return err
}

// CreatePort 创建港口并在同一事务写入审计记录，UN/LOCODE 冲突会返回可读错误。
func (s *Service) CreatePort(ctx context.Context, tenantID int64, in PortInput) (Port, error) {
	if err := validatePortInput(&in); err != nil {
		return Port{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Port{}, err
	}
	defer tx.Rollback(ctx)
	p, err := scanPort(tx.QueryRow(ctx, `INSERT INTO ports (tenant_id,unlocode,name_zh,name_en,country_code,city,timezone,aliases,remark,port_type,admin_area,latitude,longitude,has_coordinates,is_favorite,created_by,created_by_name,updated_by,updated_by_name) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$16,$17) RETURNING id,unlocode,name_zh,name_en,country_code,city,timezone,aliases,status,remark,version,port_type,admin_area,latitude,longitude,has_coordinates,is_favorite`, tenantID, in.UNLOCODE, in.NameZH, in.NameEN, in.CountryCode, in.City, in.Timezone, in.Aliases, in.Remark, in.PortType, in.AdminArea, in.Latitude, in.Longitude, in.HasCoordinates, in.IsFavorite, in.OperatorID, in.OperatorName))
	if err != nil {
		return Port{}, portUniqueConflict(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO port_change_logs (tenant_id,port_id,action,after_data,operator_id,operator_name) VALUES ($1,$2,'CREATE',$3,$4,$5)`, tenantID, p.ID, portJSON(p), in.OperatorID, in.OperatorName)
	if err != nil {
		return Port{}, err
	}
	return p, tx.Commit(ctx)
}

func (s *Service) UpdatePort(ctx context.Context, tenantID int64, in PortInput) (Port, error) {
	if err := validatePortInput(&in); err != nil {
		return Port{}, err
	}
	before, err := s.GetPort(ctx, tenantID, in.ID)
	if err != nil {
		return Port{}, err
	}
	if before.UNLOCODE != in.UNLOCODE {
		return Port{}, apierr.Conflict("MD_PORT_UNLOCODE_IMMUTABLE", "UN/LOCODE 是港口的标准身份，创建后不能直接修改")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Port{}, err
	}
	defer tx.Rollback(ctx)
	p, err := scanPort(tx.QueryRow(ctx, `UPDATE ports SET name_zh=$3,name_en=$4,country_code=$5,city=$6,timezone=$7,aliases=$8,remark=$9,port_type=$10,admin_area=$11,latitude=$12,longitude=$13,has_coordinates=$14,is_favorite=$15,version=version+1,updated_at=now(),updated_by=$16,updated_by_name=$17 WHERE tenant_id=$1 AND id=$2 AND version=$18 RETURNING id,unlocode,name_zh,name_en,country_code,city,timezone,aliases,status,remark,version,port_type,admin_area,latitude,longitude,has_coordinates,is_favorite`, tenantID, in.ID, in.NameZH, in.NameEN, in.CountryCode, in.City, in.Timezone, in.Aliases, in.Remark, in.PortType, in.AdminArea, in.Latitude, in.Longitude, in.HasCoordinates, in.IsFavorite, in.OperatorID, in.OperatorName, in.Version))
	if errors.Is(err, pgx.ErrNoRows) {
		return Port{}, apierr.Conflict("MD_PORT_VERSION_CONFLICT", "港口资料已被他人修改，请刷新后重试")
	}
	if err != nil {
		return Port{}, portUniqueConflict(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO port_change_logs (tenant_id,port_id,action,before_data,after_data,operator_id,operator_name) VALUES ($1,$2,'UPDATE',$3,$4,$5,$6)`, tenantID, p.ID, portJSON(before), portJSON(p), in.OperatorID, in.OperatorName)
	if err != nil {
		return Port{}, err
	}
	return p, tx.Commit(ctx)
}

func (s *Service) SetPortStatus(ctx context.Context, tenantID, id int64, status string, version int32, opID int64, opName string) (Port, error) {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status != "ACTIVE" && status != "INACTIVE" {
		return Port{}, apierr.Invalid("MD_PORT_STATUS_INVALID", "港口状态无效")
	}
	before, err := s.GetPort(ctx, tenantID, id)
	if err != nil {
		return Port{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Port{}, err
	}
	defer tx.Rollback(ctx)
	p, err := scanPort(tx.QueryRow(ctx, `UPDATE ports SET status=$3,version=version+1,updated_at=now(),updated_by=$4,updated_by_name=$5 WHERE tenant_id=$1 AND id=$2 AND version=$6 RETURNING id,unlocode,name_zh,name_en,country_code,city,timezone,aliases,status,remark,version,port_type,admin_area,latitude,longitude,has_coordinates,is_favorite`, tenantID, id, status, opID, opName, version))
	if errors.Is(err, pgx.ErrNoRows) {
		return Port{}, apierr.Conflict("MD_PORT_VERSION_CONFLICT", "港口资料已被他人修改，请刷新后重试")
	}
	if err != nil {
		return Port{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO port_change_logs (tenant_id,port_id,action,before_data,after_data,operator_id,operator_name) VALUES ($1,$2,$3,$4,$5,$6,$7)`, tenantID, id, status, portJSON(before), portJSON(p), opID, opName)
	if err != nil {
		return Port{}, err
	}
	return p, tx.Commit(ctx)
}
