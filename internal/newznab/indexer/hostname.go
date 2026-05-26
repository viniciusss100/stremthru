package newznab_indexer

import (
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"sync"

	"github.com/MunifTanjim/stremthru/internal/config"
	newznab_client "github.com/MunifTanjim/stremthru/internal/newznab/client"
)

var indexerIDByHostname = struct {
	sync.RWMutex
	data map[string]int64
}{data: map[string]int64{}}

func lookupIndexerIdByHostname(hostname string) (int64, bool) {
	indexerIDByHostname.RLock()
	defer indexerIDByHostname.RUnlock()
	id, ok := indexerIDByHostname.data[hostname]
	return id, ok
}

func GetHostnamesByIndexerIDMap() map[int64][]string {
	indexerIDByHostname.RLock()
	defer indexerIDByHostname.RUnlock()
	result := map[int64][]string{}
	for h, id := range indexerIDByHostname.data {
		result[id] = append(result[id], h)
	}
	for _, hostnames := range result {
		sort.Strings(hostnames)
	}
	return result
}

func ResolveIdByURL(rawURL string) int64 {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return 0
	}
	if u.Host == config.BaseURL.Host {
		return 0
	}
	if indexerId, ok := lookupIndexerIdByHostname(u.Hostname()); ok {
		return indexerId
	}
	if indexers, err := GetByHost(u.Host); err != nil {
		log.Error("failed to lookup indexer by host", "error", err)
	} else if len(indexers) > 0 {
		if len(indexers) > 1 {
			slices.SortStableFunc(indexers, func(a, b NewznabIndexer) int {
				return b.UAt.Time.Compare(a.UAt.Time)
			})
		}
		idxr := indexers[0]
		u, err := url.Parse(idxr.URL)
		if err != nil {
			return 0
		}
		indexerIDByHostname.Lock()
		indexerIDByHostname.data[u.Hostname()] = idxr.Id
		indexerIDByHostname.Unlock()
		return idxr.Id
	}
	return 0
}

func GetHTTPClientForURL(rawURL string) (*http.Client, error) {
	indexerId := ResolveIdByURL(rawURL)
	if indexerId == 0 {
		return nil, nil
	}

	cacheKey := strconv.FormatInt(indexerId, 10)
	var client newznab_client.Indexer
	if indexerCache.Get(cacheKey, &client) {
		return client.GetHTTPClient(), nil
	}

	idxr, err := GetById(indexerId)
	if err != nil || idxr == nil {
		return nil, err
	}

	client, err = idxr.GetClient()
	if err != nil {
		return nil, err
	}

	return client.GetHTTPClient(), nil
}
