package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	defaultRTEndpoint = "https://79frdp12pn-dsn.algolia.net/1/indexes/*/queries"
	defaultRTAppID    = "79FRDP12PN"
	defaultRTAPIKey   = "175588f6e5f8319b27702e4cc4013561"
)

type rtScores struct {
	Critic, Audience *float64
}

type rtCacheEntry struct {
	scores  rtScores
	expires time.Time
}

type rtQueryResponse struct {
	Results []struct {
		Hits []struct {
			Title       string          `json:"title"`
			Type        string          `json:"type"`
			ReleaseYear json.RawMessage `json:"releaseYear"`
			RT          struct {
				CriticsScore  *float64 `json:"criticsScore"`
				AudienceScore *float64 `json:"audienceScore"`
			} `json:"rottenTomatoes"`
		} `json:"hits"`
	} `json:"results"`
}

func (p *Plugin) rottenTomatoesRatings(ctx context.Context, title, itemType string, releaseYear int32) rtScores {
	p.mu.RLock()
	enabled := p.rtEnabled
	p.mu.RUnlock()
	if !enabled || strings.TrimSpace(title) == "" {
		return rtScores{}
	}
	key := fmt.Sprintf("%s|%s|%d", stremioType(itemType), normalizeRTTitle(title), releaseYear)
	now := time.Now()
	p.rtMu.Lock()
	if cached, ok := p.rtCache[key]; ok && now.Before(cached.expires) {
		p.rtMu.Unlock()
		return cached.scores
	}
	p.rtMu.Unlock()

	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	scores, err := p.queryRottenTomatoes(queryCtx, title, itemType, releaseYear)
	ttl := 24 * time.Hour
	if err != nil || (scores.Critic == nil && scores.Audience == nil) {
		ttl = 15 * time.Minute
		if err != nil {
			p.logger.Warn("Rotten Tomatoes enrichment failed", "error", err)
		}
	}
	p.rtMu.Lock()
	p.rtCache[key] = rtCacheEntry{scores: scores, expires: now.Add(ttl)}
	p.rtMu.Unlock()
	return scores
}

func (p *Plugin) queryRottenTomatoes(ctx context.Context, title, itemType string, releaseYear int32) (rtScores, error) {
	p.mu.RLock()
	endpoint, appID, apiKey := p.rtEndpoint, p.rtAppID, p.rtAPIKey
	p.mu.RUnlock()
	body := map[string]any{"requests": []map[string]string{{"indexName": "content_rt", "query": title, "params": "filters=" + url.QueryEscape("isEmsSearchable = 1") + "&hitsPerPage=20"}}}
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return rtScores{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Algolia-Application-Id", appID)
	req.Header.Set("X-Algolia-API-Key", apiKey)
	req.Header.Set("User-Agent", userAgent)
	resp, err := p.client.Do(req)
	if err != nil {
		return rtScores{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return rtScores{}, fmt.Errorf("Rotten Tomatoes search returned %s", resp.Status)
	}
	var decoded rtQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return rtScores{}, err
	}
	if len(decoded.Results) == 0 {
		return rtScores{}, nil
	}
	wantTitle, wantType := normalizeRTTitle(title), stremioType(itemType)
	bestDistance := 100
	var best rtScores
	for _, hit := range decoded.Results[0].Hits {
		if normalizeRTTitle(hit.Title) != wantTitle || !compatibleRTType(wantType, hit.Type) {
			continue
		}
		y := parseRTYear(hit.ReleaseYear)
		distance := 0
		if releaseYear > 0 && y > 0 {
			distance = abs(int(releaseYear) - y)
			if distance > 0 && !(wantType == "series" && distance == 1) {
				continue
			}
		}
		if distance < bestDistance {
			bestDistance = distance
			best = rtScores{Critic: validRTScore(hit.RT.CriticsScore), Audience: validRTScore(hit.RT.AudienceScore)}
		}
	}
	return best, nil
}

func normalizeRTTitle(value string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }), " ")
}

func compatibleRTType(want, got string) bool {
	got = strings.ToLower(strings.TrimSpace(got))
	if want == "movie" {
		return got == "movie" || got == "movies" || got == "film"
	}
	return got == "tv" || got == "series" || got == "show" || got == "tvseries"
}

func parseRTYear(raw json.RawMessage) int {
	var number int
	if json.Unmarshal(raw, &number) == nil {
		return number
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		number, _ = strconv.Atoi(text)
	}
	return number
}

func validRTScore(value *float64) *float64 {
	if value == nil || *value < 0 || *value > 100 {
		return nil
	}
	return value
}
