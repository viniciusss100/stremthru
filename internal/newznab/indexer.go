package newznab

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MunifTanjim/stremthru/internal/config"
	newznabcache "github.com/MunifTanjim/stremthru/internal/newznab/cache"
	newznab_client "github.com/MunifTanjim/stremthru/internal/newznab/client"
	newznab_indexer "github.com/MunifTanjim/stremthru/internal/newznab/indexer"
	newznab_stats "github.com/MunifTanjim/stremthru/internal/newznab/stats"
	"github.com/MunifTanjim/stremthru/internal/usenet/nzb_info"
	"github.com/MunifTanjim/stremthru/internal/util"
	"github.com/MunifTanjim/stremthru/internal/znab"
)

type Indexer interface {
	Info() znab.Info
	Search(query Query) ([]FeedItem, error)
	Download(link string, indexerId int64) (io.ReadCloser, http.Header, error)
	Capabilities() znab.Caps
}

type stremThruIndexer struct {
	info znab.Info
	caps znab.Caps
}

func (sti stremThruIndexer) Info() znab.Info {
	return sti.info
}

func (sti stremThruIndexer) Capabilities() znab.Caps {
	return sti.caps
}

func (sti stremThruIndexer) Search(q Query) ([]FeedItem, error) {
	indexers, err := newznab_indexer.GetAllEnabled()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch indexers: %w", err)
	}

	if len(indexers) == 0 {
		return []FeedItem{}, nil
	}

	type searchResult struct {
		indexer *newznab_indexer.NewznabIndexer
		items   []newznab_client.Newz
		err     error
	}

	resultCh := make(chan searchResult, len(indexers))
	var wg sync.WaitGroup

	for i := range indexers {
		idxr := &indexers[i]
		apikey, err := idxr.GetAPIKey()
		if err != nil {
			continue
		}
		wg.Add(1)
		go func(idxr *newznab_indexer.NewznabIndexer, q Query) {
			defer wg.Done()

			rl, err := idxr.GetRateLimiter()
			if err != nil {
				newznab_stats.RecordSearch(idxr.Id, 0, 0, 0, err)
				resultCh <- searchResult{indexer: idxr, err: fmt.Errorf("failed to get rate limiter: %w", err)}
				return
			}
			if rl != nil {
				if result, err := rl.Try(); err != nil {
					newznab_stats.RecordSearch(idxr.Id, 0, 0, 0, err)
					resultCh <- searchResult{indexer: idxr, err: fmt.Errorf("rate limiter error: %w", err)}
					return
				} else if !result.Allowed {
					newznab_stats.RecordRateLimited(idxr.Id, newznab_stats.OperationSearch)
					resultCh <- searchResult{indexer: idxr, err: errors.New("rate limit exceeded")}
					return
				}
			}

			client, err := idxr.GetClient()
			if err != nil {
				newznab_stats.RecordSearch(idxr.Id, 0, 0, 0, err)
				resultCh <- searchResult{indexer: idxr, err: err}
				return
			}

			var headers http.Header
			switch {
			case q.HasMovies():
				headers = config.Newz.IndexerRequestHeader.Query.Get(config.NewzIndexerRequestQueryTypeMovie)
			case q.HasTVShows():
				headers = config.Newz.IndexerRequestHeader.Query.Get(config.NewzIndexerRequestQueryTypeTV)
			default:
				headers = config.Newz.IndexerRequestHeader.Query.Get(config.NewzIndexerRequestQueryTypeAny)
			}
			caps, err := client.GetCaps()
			if err != nil {
				newznab_stats.RecordSearch(idxr.Id, 0, 0, 0, err)
				resultCh <- searchResult{indexer: idxr, err: fmt.Errorf("failed to get capabilities: %w", err)}
				return
			}
			adjQ, err := adjustQueryForCaps(q, caps)
			if err != nil {
				newznab_stats.RecordSearch(idxr.Id, 0, 0, 0, err)
				resultCh <- searchResult{indexer: idxr, err: err}
				return
			}
			query := adjQ.ToValues()
			query.Set("apikey", apikey)
			items, err := newznabcache.Search.Do(idxr.Id, client, query, headers, log)
			resultCh <- searchResult{indexer: idxr, items: items, err: err}
		}(idxr, q.Clone())
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	allItems := []FeedItem{}
	for result := range resultCh {
		if result.err != nil {
			continue
		}
		hostnames := util.NewSet[string]()
		for _, item := range result.items {
			feedItem := convertToFeedItem(item, result.indexer)
			allItems = append(allItems, feedItem)
			if u, err := url.Parse(item.DownloadLink); err == nil {
				if h := u.Hostname(); h != "" {
					hostnames.Add(h)
				}
			}
		}
		newznab_indexer.RecordHostnames(result.indexer.Id, hostnames)
	}

	if q.Offset >= len(allItems) {
		allItems = []FeedItem{}
	} else if q.Offset > 0 {
		allItems = allItems[q.Offset:]
	}
	if q.Limit > 0 && q.Limit < len(allItems) {
		allItems = allItems[:q.Limit]
	}

	return allItems, nil
}

func (sti stremThruIndexer) SearchSingle(idxr *newznab_indexer.NewznabIndexer, q Query) ([]FeedItem, error) {
	apikey, err := idxr.GetAPIKey()
	if err != nil {
		return nil, fmt.Errorf("failed to get api key: %w", err)
	}

	rl, err := idxr.GetRateLimiter()
	if err != nil {
		newznab_stats.RecordSearch(idxr.Id, 0, 0, 0, err)
		return nil, fmt.Errorf("failed to get rate limiter: %w", err)
	}
	if rl != nil {
		if result, err := rl.Try(); err != nil {
			newznab_stats.RecordSearch(idxr.Id, 0, 0, 0, err)
			return nil, fmt.Errorf("rate limiter error: %w", err)
		} else if !result.Allowed {
			newznab_stats.RecordRateLimited(idxr.Id, newznab_stats.OperationSearch)
			return nil, errors.New("rate limit exceeded")
		}
	}

	client, err := idxr.GetClient()
	if err != nil {
		newznab_stats.RecordSearch(idxr.Id, 0, 0, 0, err)
		return nil, err
	}

	var headers http.Header
	switch {
	case q.HasMovies():
		headers = config.Newz.IndexerRequestHeader.Query.Get(config.NewzIndexerRequestQueryTypeMovie)
	case q.HasTVShows():
		headers = config.Newz.IndexerRequestHeader.Query.Get(config.NewzIndexerRequestQueryTypeTV)
	default:
		headers = config.Newz.IndexerRequestHeader.Query.Get(config.NewzIndexerRequestQueryTypeAny)
	}

	caps, err := client.GetCaps()
	if err != nil {
		newznab_stats.RecordSearch(idxr.Id, 0, 0, 0, err)
		return nil, fmt.Errorf("failed to get capabilities: %w", err)
	}

	adjQ, err := adjustQueryForCaps(q, caps)
	if err != nil {
		newznab_stats.RecordSearch(idxr.Id, 0, 0, 0, err)
		return nil, err
	}

	query := adjQ.ToValues()
	query.Set("apikey", apikey)

	items, err := newznabcache.Search.Do(idxr.Id, client, query, headers, log)
	if err != nil {
		return nil, err
	}

	allItems := make([]FeedItem, 0, len(items))
	hostnames := util.NewSet[string]()
	for _, item := range items {
		feedItem := convertToFeedItem(item, idxr)
		allItems = append(allItems, feedItem)
		if u, err := url.Parse(item.DownloadLink); err == nil {
			if h := u.Hostname(); h != "" {
				hostnames.Add(h)
			}
		}
	}
	newznab_indexer.RecordHostnames(idxr.Id, hostnames)

	return allItems, nil
}

func convertToFeedItem(n newznab_client.Newz, indexer *newznab_indexer.NewznabIndexer) FeedItem {
	guid := strconv.FormatInt(indexer.Id, 10) + ":" + util.Base64Encode(n.DownloadLink)

	category := CategoryOther
	if len(n.Categories) > 0 {
		if catId := util.SafeParseInt(n.Categories[0], 0); catId > 0 {
			category = ParentCategory(Category{ID: catId})
		}
	}

	item := FeedItem{
		Title:       n.Title,
		GUID:        guid,
		PublishDate: n.PublishDate,
		Size:        n.Size,
		Link:        n.DownloadLink,
		Files:       n.Files,
		Poster:      n.Poster,
		Group:       n.Group,
		Grabs:       n.Grabs,
		Comments:    n.Comments,
		Password:    n.Password,
		UsenetDate:  n.Date,
		IMDB:        n.IMDB,
		Season:      n.Season,
		Episode:     n.Episode,
		Category:    category,
	}
	item.Indexer = ChannelItemIndexer(n.Indexer)
	if item.Indexer.Host == "" {
		item.Indexer.Host = indexer.GetHost()
	}
	if item.Indexer.Name == "" {
		item.Indexer.Name = indexer.Name
	}
	return item
}

func parseNZBId(nzbId string) (indexerId int64, downloadURL string, err error) {
	parts := strings.SplitN(nzbId, ":", 2)
	if len(parts) != 2 {
		return 0, "", errors.New("invalid nzb id format")
	}
	indexerId, err = strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", errors.New("invalid indexer id")
	}
	downloadURL, err = util.Base64Decode(parts[1])
	if err != nil {
		return 0, "", errors.New("invalid encoded url")
	}
	return indexerId, downloadURL, nil
}

func (sti stremThruIndexer) UnwrapLink(id string) (int64, *url.URL, error) {
	indexerId, link, err := parseNZBId(id)
	if err != nil {
		return 0, nil, err
	}

	indexer, err := newznab_indexer.GetById(indexerId)
	if err != nil {
		return indexerId, nil, fmt.Errorf("failed to get indexer: %w", err)
	}
	if indexer == nil {
		return indexerId, nil, errors.New("indexer not found")
	}

	rl, err := indexer.GetRateLimiter()
	if err != nil {
		return indexerId, nil, err
	}
	if rl != nil {
		if result, err := rl.Try(); err != nil {
			return indexerId, nil, err
		} else if !result.Allowed {
			newznab_stats.RecordRateLimited(indexerId, newznab_stats.OperationDownload)
			return indexerId, nil, errors.New("rate limit exceeded")
		}
	}

	u, err := url.Parse(link)
	if err != nil {
		return indexerId, nil, err
	}

	return indexerId, u, nil
}

func (sti stremThruIndexer) Download(link string, indexerId int64) (io.ReadCloser, http.Header, error) {
	file, err := nzb_info.FetchNZBFile(link, "", log,
		nzb_info.WithIndexerId(indexerId),
		nzb_info.WithOnFetched(func(nzbFile *nzb_info.NZBFile, err error, latency time.Duration) {
			var bytes int64
			if nzbFile != nil {
				bytes = int64(len(nzbFile.Blob))
			}
			newznab_stats.RecordDownload(indexerId, latency, bytes, err)
		}),
	)
	if err != nil {
		return nil, nil, err
	}

	body := io.NopCloser(bytes.NewReader(file.Blob))
	header := http.Header{}
	header.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, file.Name))
	header.Set("Content-Type", "application/x-nzb")
	header.Set("Content-Length", strconv.FormatInt(int64(len(file.Blob)), 10))

	return body, header, nil
}

var StremThruIndexer = stremThruIndexer{
	info: znab.Info{
		Title:       "StremThru",
		Description: "StremThru Newznab",
	},
	caps: znab.Caps{
		Server: &znab.CapsServer{
			Title:     "StremThru",
			Strapline: "StremThru Newznab",
			Image:     "https://emojiapi.dev/api/v1/sparkles/256.png",
			URL:       config.BaseURL.String(),
			Version:   "1.0",
		},
		Searching: &znab.CapsSearching{
			Search: &znab.CapsSearchingItem{
				Available:       true,
				SupportedParams: []znab.SearchParam{znab.SearchParamQ},
			},
			TVSearch: &znab.CapsSearchingItem{
				Available:       true,
				SupportedParams: []znab.SearchParam{znab.SearchParamQ, znab.SearchParamIMDBId, znab.SearchParamSeason, znab.SearchParamEp},
			},
			MovieSearch: &znab.CapsSearchingItem{
				Available:       true,
				SupportedParams: []znab.SearchParam{znab.SearchParamQ, znab.SearchParamIMDBId},
			},
		},
		Categories: []znab.CapsCategory{
			{Category: CategoryMovies},
			{Category: CategoryTV},
		},
	},
}
