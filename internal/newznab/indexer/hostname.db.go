package newznab_indexer

import (
	"fmt"

	"github.com/MunifTanjim/stremthru/internal/db"
	"github.com/MunifTanjim/stremthru/internal/util"
)

const HostnameTableName = "newznab_indexer_hostname"

var HostnameColumn = struct {
	Hostname  string
	IndexerID string
	CAt       string
	UAt       string
}{
	Hostname:  "hostname",
	IndexerID: "indexer_id",
	CAt:       "cat",
	UAt:       "uat",
}

var HostnameColumns = []string{
	HostnameColumn.Hostname,
	HostnameColumn.IndexerID,
	HostnameColumn.CAt,
	HostnameColumn.UAt,
}

var query_load_hostname_map = fmt.Sprintf(
	`SELECT %s, %s FROM %s`,
	HostnameColumn.Hostname,
	HostnameColumn.IndexerID,
	HostnameTableName,
)

func LoadIndexerIDByHostnameMap() error {
	rows, err := db.Query(query_load_hostname_map)
	if err != nil {
		return err
	}
	defer rows.Close()

	data := map[string]int64{}
	for rows.Next() {
		var hostname string
		var indexerId int64
		if err := rows.Scan(&hostname, &indexerId); err != nil {
			return err
		}
		data[hostname] = indexerId
	}
	if err := rows.Err(); err != nil {
		return err
	}

	indexerIDByHostname.Lock()
	indexerIDByHostname.data = data
	indexerIDByHostname.Unlock()
	return nil
}

var query_upsert_hostnames = fmt.Sprintf(
	`INSERT INTO %s (%s) VALUES (?, ?) ON CONFLICT (%s) DO UPDATE SET %s = EXCLUDED.%s, %s = %s`,
	HostnameTableName,
	db.JoinColumnNames(HostnameColumn.Hostname, HostnameColumn.IndexerID),
	HostnameColumn.Hostname,
	HostnameColumn.IndexerID,
	HostnameColumn.IndexerID,
	HostnameColumn.UAt,
	db.CurrentTimestamp,
)

func RecordHostnames(indexerId int64, hostnames *util.Set[string]) {
	if indexerId == 0 || hostnames.Size() == 0 {
		return
	}

	hostnamesToWrite := make([]string, 0, hostnames.Size())

	indexerIDByHostname.RLock()
	for h := range hostnames.Seq() {
		if h == "" {
			continue
		}
		if existing, ok := indexerIDByHostname.data[h]; ok && existing == indexerId {
			continue
		}
		hostnamesToWrite = append(hostnamesToWrite, h)
	}
	indexerIDByHostname.RUnlock()

	if len(hostnamesToWrite) == 0 {
		return
	}

	writtenHostnames := make([]string, 0, len(hostnamesToWrite))
	for _, h := range hostnamesToWrite {
		_, err := db.Exec(query_upsert_hostnames, h, indexerId)
		if err != nil {
			log.Error("failed to upsert hostname for indexer", "error", err, "hostname", h, "indexer_id", indexerId)
			continue
		}
		writtenHostnames = append(writtenHostnames, h)
	}

	if len(writtenHostnames) > 0 {
		indexerIDByHostname.Lock()
		for _, h := range writtenHostnames {
			indexerIDByHostname.data[h] = indexerId
		}
		indexerIDByHostname.Unlock()
	}
}

var query_delete_hostnames_by_indexer_id = fmt.Sprintf(
	`DELETE FROM %s WHERE %s = ?`,
	HostnameTableName,
	HostnameColumn.IndexerID,
)

func deleteHostnamesByIndexerID(indexerID int64) error {
	if _, err := db.Exec(query_delete_hostnames_by_indexer_id, indexerID); err != nil {
		return err
	}

	indexerIDByHostname.Lock()
	for h, id := range indexerIDByHostname.data {
		if id == indexerID {
			delete(indexerIDByHostname.data, h)
		}
	}
	indexerIDByHostname.Unlock()
	return nil
}
