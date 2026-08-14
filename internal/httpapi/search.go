package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"

	"capsnap/internal/embedding"
	"capsnap/internal/store"
)

const rrfK = 60

type searchResponse struct {
	Items         []store.Screenshot `json:"items"`
	SearchMode    string             `json:"search_mode"`
	KeywordCount  int                `json:"keyword_count"`
	SemanticCount int                `json:"semantic_count"`
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	deviceID := firstNonEmpty(r.URL.Query().Get("device_id"), "demo-device")
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeError(w, http.StatusBadRequest, errors.New("搜索关键词不能为空"))
		return
	}
	mode := firstNonEmpty(r.URL.Query().Get("mode"), "hybrid")
	limit := parseLimit(r.URL.Query().Get("limit"), 20)

	result, err := s.doSearch(r.Context(), deviceID, query, mode, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("X-Search-Mode", result.SearchMode)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) doSearch(ctx context.Context, deviceID, query, mode string, limit int) (*searchResponse, error) {
	switch mode {
	case "keyword":
		items, err := s.store.SearchScreenshots(ctx, deviceID, query, limit)
		if err != nil {
			return nil, err
		}
		return &searchResponse{Items: items, SearchMode: "keyword", KeywordCount: len(items)}, nil

	case "semantic":
		items, n, err := s.semanticSearch(ctx, deviceID, query, limit)
		if err != nil {
			kw, kwErr := s.store.SearchScreenshots(ctx, deviceID, query, limit)
			if kwErr != nil {
				return nil, kwErr
			}
			return &searchResponse{Items: kw, SearchMode: "keyword", KeywordCount: len(kw)}, nil
		}
		return &searchResponse{Items: items, SearchMode: "semantic", SemanticCount: n}, nil

	default:
		return s.hybridSearch(ctx, deviceID, query, limit)
	}
}

func (s *Server) hybridSearch(ctx context.Context, deviceID, query string, limit int) (*searchResponse, error) {
	fetchLimit := limit * 2
	if fetchLimit < 20 {
		fetchLimit = 20
	}

	var (
		kwItems []store.Screenshot
		kwErr   error
		semIDs  []rankedID
		semErr  error
		wg      sync.WaitGroup
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		kwItems, kwErr = s.store.SearchScreenshots(ctx, deviceID, query, fetchLimit)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		semIDs, semErr = s.semanticRankedIDs(ctx, deviceID, query, fetchLimit)
	}()
	wg.Wait()

	if kwErr != nil {
		return nil, kwErr
	}

	if semErr != nil || s.embedder == nil || s.vectors == nil {
		return &searchResponse{
			Items:        limitShots(kwItems, limit),
			SearchMode:   "keyword",
			KeywordCount: len(kwItems),
		}, nil
	}

	kwRanks := make([]rankedID, len(kwItems))
	for i, item := range kwItems {
		kwRanks[i] = rankedID{ID: item.ID, Rank: i + 1}
	}

	mergedIDs := rrfMerge(kwRanks, semIDs, rrfK, limit)
	items, err := s.store.GetScreenshotsByIDs(ctx, mergedIDs)
	if err != nil {
		return nil, err
	}
	return &searchResponse{
		Items:         items,
		SearchMode:    "hybrid",
		KeywordCount:  len(kwItems),
		SemanticCount: len(semIDs),
	}, nil
}

func (s *Server) semanticSearch(ctx context.Context, deviceID, query string, limit int) ([]store.Screenshot, int, error) {
	ids, err := s.semanticRankedIDs(ctx, deviceID, query, limit)
	if err != nil {
		return nil, 0, err
	}
	idList := make([]string, len(ids))
	for i, r := range ids {
		idList[i] = r.ID
	}
	items, err := s.store.GetScreenshotsByIDs(ctx, idList)
	if err != nil {
		return nil, 0, err
	}
	return items, len(ids), nil
}

func (s *Server) semanticRankedIDs(ctx context.Context, deviceID, query string, limit int) ([]rankedID, error) {
	if s.embedder == nil || s.vectors == nil {
		return nil, errors.New("语义搜索未启用")
	}
	vec, err := s.embedder.Embed(ctx, query, embedding.TextTypeQuery)
	if err != nil {
		return nil, err
	}
	results, err := s.vectors.Search(ctx, vec, limit, map[string]any{"device_id": deviceID})
	if err != nil {
		return nil, err
	}
	ranked := make([]rankedID, len(results))
	for i, r := range results {
		ranked[i] = rankedID{ID: r.ID, Rank: i + 1}
	}
	return ranked, nil
}

type rankedID struct {
	ID   string
	Rank int
}

func rrfMerge(a, b []rankedID, k, limit int) []string {
	scores := map[string]float64{}
	for _, list := range [][]rankedID{a, b} {
		for _, item := range list {
			if item.ID == "" {
				continue
			}
			scores[item.ID] += 1.0 / float64(k+item.Rank)
		}
	}
	type scored struct {
		id    string
		score float64
	}
	items := make([]scored, 0, len(scores))
	for id, score := range scores {
		items = append(items, scored{id: id, score: score})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return items[i].id < items[j].id
		}
		return items[i].score > items[j].score
	})
	if limit <= 0 || limit > len(items) {
		limit = len(items)
	}
	out := make([]string, limit)
	for i := 0; i < limit; i++ {
		out[i] = items[i].id
	}
	return out
}

func limitShots(items []store.Screenshot, limit int) []store.Screenshot {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}
