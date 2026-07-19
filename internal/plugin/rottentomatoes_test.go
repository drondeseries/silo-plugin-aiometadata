package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/go-hclog"
)

func TestQueryRottenTomatoesMatchesTitleTypeAndYear(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Algolia-Application-Id") != defaultRTAppID {
			t.Error("missing app id")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"results":[{"hits":[
			{"title":"Alien","type":"movie","releaseYear":2024,"rottenTomatoes":{"criticsScore":20,"audienceScore":30}},
			{"title":"Alien","type":"tv","releaseYear":1979,"rottenTomatoes":{"criticsScore":40,"audienceScore":50}},
			{"title":"Alien","type":"movie","releaseYear":"1979","rottenTomatoes":{"criticsScore":93,"audienceScore":94}}
		]}]}`))
	}))
	defer server.Close()
	p := New(hclog.NewNullLogger(), server.Client())
	p.rtEndpoint = server.URL
	scores, err := p.queryRottenTomatoes(context.Background(), "Alien", "movie", 1979)
	if err != nil || scores.Critic == nil || *scores.Critic != 93 || scores.Audience == nil || *scores.Audience != 94 {
		t.Fatalf("scores=%+v err=%v", scores, err)
	}
}

func TestQueryRottenTomatoesRejectsLooseAndWrongYearMatches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"hits":[
			{"title":"The Alien","type":"movie","releaseYear":1979,"rottenTomatoes":{"criticsScore":99}},
			{"title":"Alien","type":"movie","releaseYear":2024,"rottenTomatoes":{"criticsScore":20}}
		]}]}`))
	}))
	defer server.Close()
	p := New(hclog.NewNullLogger(), server.Client())
	p.rtEndpoint = server.URL
	scores, err := p.queryRottenTomatoes(context.Background(), "Alien", "movie", 1979)
	if err != nil || scores.Critic != nil || scores.Audience != nil {
		t.Fatalf("scores=%+v err=%v", scores, err)
	}
}

func TestQueryRottenTomatoesAllowsOneYearSeriesDifference(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"hits":[{"title":"One Piece","type":"tv","releaseYear":1998,"rottenTomatoes":{"audienceScore":95}}]}]}`))
	}))
	defer server.Close()
	p := New(hclog.NewNullLogger(), server.Client())
	p.rtEndpoint = server.URL
	scores, err := p.queryRottenTomatoes(context.Background(), "One Piece", "series", 1999)
	if err != nil || scores.Audience == nil || *scores.Audience != 95 {
		t.Fatalf("scores=%+v err=%v", scores, err)
	}
}
