package newznab_indexer

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/MunifTanjim/stremthru/internal/cache"
	"github.com/MunifTanjim/stremthru/internal/config"
	newznab_client "github.com/MunifTanjim/stremthru/internal/newznab/client"
)

var indexerCache = cache.NewLRUCache[newznab_client.Indexer](&cache.CacheConfig{
	Name:     "newznab:indexer",
	Lifetime: 3 * time.Hour,
})

func invalidateClient(id int64) {
	indexerCache.Remove(strconv.FormatInt(id, 10))
}

func (idxr *NewznabIndexer) getHTTPClient() *http.Client {
	if !idxr.Tunnel.Valid {
		return config.GetHTTPClient(config.TUNNEL_TYPE_AUTO)
	}
	switch idxr.Tunnel.String {
	case "":
		return config.GetHTTPClient(config.TUNNEL_TYPE_AUTO)
	case TunnelForced:
		return config.GetHTTPClient(config.TUNNEL_TYPE_FORCED)
	case TunnelNone:
		return config.GetHTTPClient(config.TUNNEL_TYPE_NONE)
	}
	u, err := url.Parse(idxr.Tunnel.String)
	if err != nil || u.Scheme == "" || u.Host == "" {
		log.Warn("invalid tunnel value, falling back to auto", "tunnel", idxr.Tunnel.String, "indexer_id", idxr.Id)
		return config.GetHTTPClient(config.TUNNEL_TYPE_AUTO)
	}
	return config.GetHTTPClientWithProxy(u)
}

func (idxr *NewznabIndexer) GetClient() (newznab_client.Indexer, error) {
	switch idxr.Type {
	case IndexerTypeGeneric:
		cacheKey := strconv.FormatInt(idxr.Id, 10)
		var client newznab_client.Indexer
		if !indexerCache.Get(cacheKey, &client) {
			apiKey, err := idxr.GetAPIKey()
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt api key: %w", err)
			}

			httpClient := idxr.getHTTPClient()
			httpClient.Timeout = 60 * time.Second

			client = newznab_client.NewClient(&newznab_client.ClientConfig{
				BaseURL:    idxr.URL,
				APIKey:     apiKey,
				HTTPClient: httpClient,
			})

			if err := indexerCache.Add(cacheKey, client); err != nil {
				return nil, err
			}
		}

		return client, nil

	default:
		return nil, fmt.Errorf("invalid indexer type: %s", idxr.Type)
	}
}
