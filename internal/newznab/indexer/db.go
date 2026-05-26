package newznab_indexer

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	"github.com/MunifTanjim/stremthru/core"
	"github.com/MunifTanjim/stremthru/internal/config"
	"github.com/MunifTanjim/stremthru/internal/db"
	newznab_client "github.com/MunifTanjim/stremthru/internal/newznab/client"
	"github.com/MunifTanjim/stremthru/internal/ratelimit"
	rrl "github.com/nccapo/rate-limiter"
)

func encrypt(value string) (string, error) {
	return core.Encrypt(config.VaultSecret, value)
}

func decrypt(value string) (string, error) {
	return core.Decrypt(config.VaultSecret, value)
}

const TableName = "newznab_indexer"

type IndexerType string

const (
	IndexerTypeGeneric IndexerType = "generic"
)

const (
	TunnelForced = "true"
	TunnelNone   = "false"
)

func (it IndexerType) IsValid() bool {
	switch it {
	case IndexerTypeGeneric:
		return true
	default:
		return false
	}
}

type NewznabIndexer struct {
	Id                int64
	Type              IndexerType
	Name              string
	URL               string
	APIKey            string
	RateLimitConfigId sql.NullString
	Disabled          bool
	Tunnel            sql.NullString
	CAt               db.Timestamp
	UAt               db.Timestamp

	host   string
	apikey string
}

func (idxr *NewznabIndexer) GetHost() string {
	if idxr.host == "" {
		if u, err := url.Parse(idxr.URL); err == nil {
			idxr.host = u.Host
		}
	}
	return idxr.host
}

type newznabIndexerRateLimiter struct {
	*ratelimit.Limiter
	prefix string
}

func (rl *newznabIndexerRateLimiter) Try() (*rrl.RateLimitResult, error) {
	return rl.Limiter.Try(rl.prefix)
}

func (rl *newznabIndexerRateLimiter) Wait() error {
	return rl.Limiter.Wait(rl.prefix)
}

func (idxr NewznabIndexer) GetRateLimiter() (*newznabIndexerRateLimiter, error) {
	if !idxr.RateLimitConfigId.Valid {
		return nil, nil
	}
	rl, err := ratelimit.NewLimiterById(idxr.RateLimitConfigId.String)
	if err != nil {
		return nil, err
	}
	return &newznabIndexerRateLimiter{
		Limiter: rl,
		prefix:  fmt.Sprintf("newznab:%d", idxr.Id),
	}, nil
}

func NewNewznabIndexer(url, apiKey string) (*NewznabIndexer, error) {
	indexer := &NewznabIndexer{
		Type: IndexerTypeGeneric,
		URL:  url,
	}
	err := indexer.SetAPIKey(apiKey)
	if err != nil {
		return nil, err
	}
	return indexer, nil
}

func (idxr *NewznabIndexer) SetAPIKey(apiKey string) error {
	if apiKey == "" {
		return nil
	}
	encAPIKey, err := encrypt(apiKey)
	if err != nil {
		return err
	}
	idxr.APIKey = encAPIKey
	idxr.apikey = apiKey
	return nil
}

func (idxr *NewznabIndexer) GetAPIKey() (string, error) {
	if idxr.APIKey == "" {
		return "", nil
	}
	if idxr.apikey == "" {
		apikey, err := decrypt(idxr.APIKey)
		if err != nil {
			return "", err
		}
		idxr.apikey = apikey
	}
	return idxr.apikey, nil
}

func ValidateTunnel(value string) error {
	switch value {
	case "", TunnelForced, TunnelNone:
		return nil
	default:
		u, err := url.Parse(value)
		if err != nil {
			return fmt.Errorf("invalid tunnel: %w", err)
		}
		if u.Host == "" {
			return fmt.Errorf("invalid tunnel: missing host")
		}
		switch u.Scheme {
		case "http", "https", "socks5", "socks5h":
		case "":
			return fmt.Errorf("invalid tunnel: missing scheme")
		default:
			return fmt.Errorf("invalid tunnel: unsupported scheme '%s' (must be http, https, socks5, or socks5h)", u.Scheme)
		}
		return nil
	}
}

func (idxr *NewznabIndexer) Validate() error {
	apiKey, err := idxr.GetAPIKey()
	if err != nil {
		return fmt.Errorf("failed to decrypt api key: %w", err)
	}

	httpClient := idxr.getHTTPClient()
	if _, err := config.IP.GetIP(httpClient); err != nil {
		return fmt.Errorf("failed to resolve IP for tunnel: %w", err)
	}

	client := newznab_client.NewClient(&newznab_client.ClientConfig{
		BaseURL:    idxr.URL,
		APIKey:     apiKey,
		HTTPClient: httpClient,
	})

	caps, err := client.GetCaps()
	if err != nil {
		return fmt.Errorf("failed to fetch capabilities: %w", err)
	}

	if idxr.Name == "" && caps.Server.Title != "" {
		idxr.Name = caps.Server.Title
	}

	return nil
}

var Column = struct {
	Id                string
	Type              string
	Name              string
	URL               string
	APIKey            string
	RateLimitConfigId string
	Disabled          string
	Tunnel            string
	CAt               string
	UAt               string
}{
	Id:                "id",
	Type:              "type",
	Name:              "name",
	URL:               "url",
	APIKey:            "api_key",
	RateLimitConfigId: "rate_limit_config_id",
	Disabled:          "disabled",
	Tunnel:            "tunnel",
	CAt:               "cat",
	UAt:               "uat",
}

var columns = []string{
	Column.Id,
	Column.Type,
	Column.Name,
	Column.URL,
	Column.APIKey,
	Column.RateLimitConfigId,
	Column.Disabled,
	Column.Tunnel,
	Column.CAt,
	Column.UAt,
}

var query_get_all = fmt.Sprintf(
	`SELECT %s FROM %s`,
	strings.Join(columns, ", "),
	TableName,
)

func GetAll() ([]NewznabIndexer, error) {
	rows, err := db.Query(query_get_all)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []NewznabIndexer{}
	for rows.Next() {
		item := NewznabIndexer{}
		if err := rows.Scan(&item.Id, &item.Type, &item.Name, &item.URL, &item.APIKey, &item.RateLimitConfigId, &item.Disabled, &item.Tunnel, &item.CAt, &item.UAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

var query_get_all_enabled = fmt.Sprintf(
	`SELECT %s FROM %s WHERE %s = %s`,
	strings.Join(columns, ", "),
	TableName,
	Column.Disabled,
	db.BooleanFalse,
)

func GetAllEnabled() ([]NewznabIndexer, error) {
	rows, err := db.Query(query_get_all_enabled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []NewznabIndexer{}
	for rows.Next() {
		item := NewznabIndexer{}
		if err := rows.Scan(&item.Id, &item.Type, &item.Name, &item.URL, &item.APIKey, &item.RateLimitConfigId, &item.Disabled, &item.Tunnel, &item.CAt, &item.UAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

var query_get_by_id = fmt.Sprintf(
	`SELECT %s FROM %s WHERE %s = ?`,
	strings.Join(columns, ", "),
	TableName,
	Column.Id,
)

func GetById(id int64) (*NewznabIndexer, error) {
	row := db.QueryRow(query_get_by_id, id)

	item := NewznabIndexer{}
	if err := row.Scan(&item.Id, &item.Type, &item.Name, &item.URL, &item.APIKey, &item.RateLimitConfigId, &item.Disabled, &item.Tunnel, &item.CAt, &item.UAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

var query_get_by_host = fmt.Sprintf(
	`SELECT %s FROM %s WHERE %s = ? OR %s LIKE ? OR %s = ? OR %s LIKE ?`,
	strings.Join(columns, ", "),
	TableName,
	Column.URL, Column.URL, Column.URL, Column.URL,
)

func GetByHost(host string) ([]NewznabIndexer, error) {
	httpBase := "http://" + host
	httpsBase := "https://" + host
	rows, err := db.Query(query_get_by_host, httpBase, httpBase+"/%", httpsBase, httpsBase+"/%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []NewznabIndexer{}
	for rows.Next() {
		item := NewznabIndexer{}
		if err := rows.Scan(&item.Id, &item.Type, &item.Name, &item.URL, &item.APIKey, &item.RateLimitConfigId, &item.Disabled, &item.Tunnel, &item.CAt, &item.UAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

var query_get_by_url = fmt.Sprintf(
	`SELECT %s FROM %s WHERE %s = ?`,
	strings.Join(columns, ", "),
	TableName,
	Column.URL,
)

func GetByURL(url string) (*NewznabIndexer, error) {
	row := db.QueryRow(query_get_by_url, url)

	item := NewznabIndexer{}
	if err := row.Scan(&item.Id, &item.Type, &item.Name, &item.URL, &item.APIKey, &item.RateLimitConfigId, &item.Disabled, &item.Tunnel, &item.CAt, &item.UAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

var query_insert = fmt.Sprintf(
	`INSERT INTO %s (%s) VALUES (?,?,?,?,?,?)`,
	TableName,
	db.JoinColumnNames(
		Column.Type,
		Column.Name,
		Column.URL,
		Column.APIKey,
		Column.RateLimitConfigId,
		Column.Tunnel,
	),
)

func (idxr *NewznabIndexer) Insert() error {
	_, err := db.Exec(query_insert,
		idxr.Type,
		idxr.Name,
		idxr.URL,
		idxr.APIKey,
		idxr.RateLimitConfigId,
		idxr.Tunnel,
	)
	if err != nil {
		return err
	}
	inserted, err := GetByURL(idxr.URL)
	if err != nil {
		return err
	}
	idxr.Id = inserted.Id
	return nil
}

var query_update = fmt.Sprintf(
	`UPDATE %s SET %s WHERE %s = ?`,
	TableName,
	strings.Join([]string{
		fmt.Sprintf(`%s = ?`, Column.Name),
		fmt.Sprintf(`%s = ?`, Column.URL),
		fmt.Sprintf(`%s = ?`, Column.APIKey),
		fmt.Sprintf(`%s = ?`, Column.RateLimitConfigId),
		fmt.Sprintf(`%s = ?`, Column.Disabled),
		fmt.Sprintf(`%s = ?`, Column.Tunnel),
		fmt.Sprintf(`%s = %s`, Column.UAt, db.CurrentTimestamp),
	}, ", "),
	Column.Id,
)

func (idxr *NewznabIndexer) Update() error {
	_, err := db.Exec(query_update,
		idxr.Name,
		idxr.URL,
		idxr.APIKey,
		idxr.RateLimitConfigId,
		idxr.Disabled,
		idxr.Tunnel,
		idxr.Id,
	)
	if err != nil {
		return err
	}
	invalidateClient(idxr.Id)
	return nil
}

var query_set_disabled = fmt.Sprintf(
	`UPDATE %s SET %s = ?, %s = %s WHERE %s = ?`,
	TableName,
	Column.Disabled,
	Column.UAt, db.CurrentTimestamp,
	Column.Id,
)

func SetDisabled(id int64, disabled bool) error {
	_, err := db.Exec(query_set_disabled, disabled, id)
	return err
}

var query_delete = fmt.Sprintf(
	`DELETE FROM %s WHERE %s = ?`,
	TableName,
	Column.Id,
)

func Delete(id int64) error {
	_, err := db.Exec(query_delete, id)
	if err != nil {
		return err
	}
	if err := deleteHostnamesByIndexerID(id); err != nil {
		log.Error("failed to delete indexer hostnames", "error", err, "indexer_id", id)
	}
	invalidateClient(id)
	return nil
}
