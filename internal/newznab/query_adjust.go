package newznab

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/MunifTanjim/stremthru/internal/imdb_title"
	"github.com/MunifTanjim/stremthru/internal/util"
	"github.com/MunifTanjim/stremthru/internal/znab"
)

func adjustQueryForCaps(query Query, caps znab.Caps) (Query, error) {
	q := query.Clone()

	if caps.Searching == nil {
		return q, errors.New("indexer capabilities missing `searching` block")
	}

	isMovie := query.Type == string(znab.FunctionSearchMovie)
	isTV := query.Type == string(znab.FunctionSearchTV)

	t := znab.Function(q.Type)
	if t == "" {
		t = znab.FunctionSearch
	}
	if (t == znab.FunctionSearchMovie || t == znab.FunctionSearchTV) && !caps.SupportsFunction(t) {
		t = znab.FunctionSearch
	}
	if !caps.SupportsFunction(t) {
		return q, fmt.Errorf("indexer does not support '%s' function", t)
	}
	q.Type = string(t)

	var terms []string

	if q.IMDBId != "" && !caps.SupportsParam(t, znab.SearchParamIMDBId) {
		title, err := imdb_title.Get(q.IMDBId)
		if err != nil {
			return q, fmt.Errorf("failed to resolve title for imdb id %q: %w", q.IMDBId, err)
		} else if title != nil && title.Title != "" {
			terms = append(terms, title.Title)
		}
		q.IMDBId = ""
	}

	seasonOK := caps.SupportsParam(t, znab.SearchParamSeason)
	epOK := caps.SupportsParam(t, znab.SearchParamEp)
	if q.Season != "" && !seasonOK {
		seasonInt, _ := strconv.Atoi(q.Season)
		term := "S" + util.ZeroPadInt(seasonInt, 2)
		q.Season = ""
		if q.Ep != "" {
			epInt, _ := strconv.Atoi(q.Ep)
			term += "E" + util.ZeroPadInt(epInt, 2)
			q.Ep = ""
		}
		terms = append(terms, term)
	} else if q.Ep != "" && !epOK {
		epInt, _ := strconv.Atoi(q.Ep)
		term := "E" + util.ZeroPadInt(epInt, 2)
		terms = append(terms, term)
		q.Ep = ""
	}

	if len(terms) > 0 {
		if q.Q != "" {
			q.Q = q.Q + " " + strings.Join(terms, " ")
		} else {
			q.Q = strings.Join(terms, " ")
		}
	}

	if q.Q != "" && !caps.SupportsParam(t, znab.SearchParamQ) {
		return q, fmt.Errorf("indexer does not support 'q' param for '%s' function", t)
	}

	if q.Q == "" && q.IMDBId == "" {
		return q, errors.New("unusable query after adjustment: 'q' and 'imdbid' both empty")
	}

	if isMovie && !hasCategoryInRange(q.Categories, CategoryMovies.ID, CategoryMovies.ID+1000) {
		q.Categories = append(q.Categories, CategoryMovies.ID)
	}
	if isTV && !hasCategoryInRange(q.Categories, CategoryTV.ID, CategoryTV.ID+1000) {
		q.Categories = append(q.Categories, CategoryTV.ID)
	}

	return q, nil
}

func hasCategoryInRange(cats []int, lo, hi int) bool {
	for _, c := range cats {
		if c >= lo && c < hi {
			return true
		}
	}
	return false
}
