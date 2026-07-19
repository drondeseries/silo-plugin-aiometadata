package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"github.com/hashicorp/go-hclog"
	"google.golang.org/protobuf/types/known/structpb"
)

func configuredPlugin(t *testing.T, handler http.Handler) *Plugin {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	p := New(hclog.NewNullLogger(), s.Client())
	cfg, _ := structpb.NewStruct(map[string]any{"addon_url": s.URL + "/manifest.json"})
	_, err := p.Configure(context.Background(), &pluginv1.ConfigureRequest{Config: []*pluginv1.ConfigEntry{{Key: "aiometadata_config", Value: cfg}}})
	if err != nil {
		t.Fatal(err)
	}
	p.rtEndpoint = s.URL
	return p
}
func TestSearchAndMetadata(t *testing.T) {
	p := configuredPlugin(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			w.Write([]byte(`{"catalogs":[{"type":"movie","id":"search","extra":[{"name":"search"}]}]}`))
		case "/catalog/movie/search/search=Alien.json":
			w.Write([]byte(`{"metas":[{"id":"tt0078748","type":"movie","name":"Alien","releaseInfo":"1979","poster":"https://img/poster.jpg"}]}`))
		case "/meta/movie/tt0078748.json":
			w.Write([]byte(`{"meta":{"id":"tt0078748","type":"movie","name":"Alien","description":"In space...","releaseInfo":"1979","released":"1979-05-25T00:00:00.000Z","runtime":"117 min","genres":["Horror"],"imdbRating":"8.5","director":["Ridley Scott"],"poster":"https://img/poster.jpg","background":"https://img/backdrop.jpg","logo":"https://img/logo.png"}}`))
		case "/":
			w.Write([]byte(`{"results":[{"hits":[{"title":"Alien","type":"movie","releaseYear":1979,"rottenTomatoes":{"criticsScore":93,"audienceScore":94}}]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	s, err := p.Search(context.Background(), &pluginv1.SearchMetadataRequest{Query: "Alien", ItemType: "movie", Year: 1979})
	if err != nil || len(s.Results) != 1 || s.Results[0].ProviderId != "tt0078748" {
		t.Fatalf("search=%+v err=%v", s, err)
	}
	g, err := p.GetMetadata(context.Background(), &pluginv1.GetMetadataRequest{ProviderId: "tt0078748", ItemType: "movie"})
	if err != nil || g.GetItem().GetRuntime() != 117 || g.GetItem().GetProviderIds().GetFields()["imdb"].GetStringValue() != "tt0078748" || g.GetItem().GetRatings().GetFields()["rt_critic"].GetNumberValue() != 93 || g.GetItem().GetRatings().GetFields()["rt_audience"].GetNumberValue() != 94 {
		t.Fatalf("metadata=%+v err=%v", g, err)
	}
	images, err := p.GetImages(context.Background(), &pluginv1.GetImagesRequest{ProviderId: "tt0078748", ItemType: "movie", Language: "en"})
	if err != nil || len(images.GetImages()) != 3 || images.GetImages()[0].GetKind() != "poster" || images.GetImages()[0].GetUrl() != "https://img/poster.jpg" {
		t.Fatalf("images=%+v err=%v", images, err)
	}
}
func TestNormalizeURL(t *testing.T) {
	got, err := normalizeURL("https://example.test/stremio/u/c/manifest.json?x=1")
	if err != nil || got != "https://example.test/stremio/u/c" {
		t.Fatalf("got %q err %v", got, err)
	}
}
