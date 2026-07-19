package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtimedefault"
	"github.com/hashicorp/go-hclog"
	"google.golang.org/protobuf/types/known/structpb"
)

const userAgent = "silo-plugin-aiometadata/0.1"

type Plugin struct {
	runtimedefault.Server
	pluginv1.UnimplementedMetadataProviderServer
	logger                        hclog.Logger
	client                        *http.Client
	mu                            sync.RWMutex
	manifest                      *pluginv1.PluginManifest
	baseURL, credential           string
	rtEndpoint, rtAppID, rtAPIKey string
	rtEnabled                     bool
	rtMu                          sync.Mutex
	rtCache                       map[string]rtCacheEntry
}

func New(logger hclog.Logger, client *http.Client) *Plugin {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &Plugin{logger: logger, client: client, rtEndpoint: defaultRTEndpoint, rtAppID: defaultRTAppID, rtAPIKey: defaultRTAPIKey, rtCache: make(map[string]rtCacheEntry), rtEnabled: true}
}
func (p *Plugin) SetManifest(m *pluginv1.PluginManifest) { p.manifest = m }
func (p *Plugin) GetManifest(context.Context, *pluginv1.GetManifestRequest) (*pluginv1.GetManifestResponse, error) {
	return &pluginv1.GetManifestResponse{Manifest: p.manifest}, nil
}

func (p *Plugin) Configure(_ context.Context, req *pluginv1.ConfigureRequest) (*pluginv1.ConfigureResponse, error) {
	var raw, credential string
	timeout := 20
	for _, e := range req.GetConfig() {
		if e.GetKey() == "aiometadata_config" {
			f := e.GetValue().GetFields()
			raw = f["addon_url"].GetStringValue()
			credential = f["api_key"].GetStringValue()
			if n := int(f["timeout_seconds"].GetNumberValue()); n > 0 && n <= 120 {
				timeout = n
			}
		}
	}
	base, err := normalizeURL(raw)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.baseURL = base
	p.credential = strings.TrimSpace(credential)
	p.client.Timeout = time.Duration(timeout) * time.Second
	p.rtEndpoint, p.rtAppID, p.rtAPIKey, p.rtEnabled = defaultRTEndpoint, defaultRTAppID, defaultRTAPIKey, true
	for _, e := range req.GetConfig() {
		if e.GetKey() != "aiometadata_config" {
			continue
		}
		f := e.GetValue().GetFields()
		if v := strings.TrimSpace(f["rt_endpoint"].GetStringValue()); v != "" {
			p.rtEndpoint = v
		}
		if v := strings.TrimSpace(f["rt_app_id"].GetStringValue()); v != "" {
			p.rtAppID = v
		}
		if v := strings.TrimSpace(f["rt_api_key"].GetStringValue()); v != "" {
			p.rtAPIKey = v
		}
		if v, ok := f["enable_rt_ratings"]; ok {
			p.rtEnabled = v.GetBoolValue()
		}
	}
	p.mu.Unlock()
	return &pluginv1.ConfigureResponse{}, nil
}

func normalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("AIOMetadata addon URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid AIOMetadata addon URL")
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.Path = strings.TrimSuffix(u.Path, "/manifest.json")
	return strings.TrimSuffix(u.String(), "/"), nil
}

type addonManifest struct {
	Catalogs []struct {
		Type, ID string
		Extra    []struct {
			Name       string
			IsRequired bool `json:"isRequired"`
		}
	} `json:"catalogs"`
}
type metaEnvelope struct {
	Meta stremioMeta `json:"meta"`
}
type catalogEnvelope struct {
	Metas []stremioMeta `json:"metas"`
}
type stremioMeta struct {
	ID, Type, Name, Description, Poster, Background, Logo, ReleaseInfo, Released, Runtime, Language, Country, IMDbRating string
	Genres                                                                                                               []string `json:"genres"`
	Director                                                                                                             any      `json:"director"`
	Cast                                                                                                                 any      `json:"cast"`
	Writer                                                                                                               any      `json:"writer"`
}

func (p *Plugin) getJSON(ctx context.Context, endpoint string, dst any) error {
	p.mu.RLock()
	credential := p.credential
	p.mu.RUnlock()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	r.Header.Set("User-Agent", userAgent)
	r.Header.Set("Accept", "application/json")
	if credential != "" {
		if x := strings.SplitN(credential, ":", 2); len(x) == 2 {
			r.SetBasicAuth(x[0], x[1])
		} else {
			r.Header.Set("Authorization", "Bearer "+credential)
			r.Header.Set("X-API-Key", credential)
		}
	}
	resp, err := p.client.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("AIOMetadata returned %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
func (p *Plugin) configured() (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.baseURL == "" {
		return "", errors.New("plugin is not configured")
	}
	return p.baseURL, nil
}

func (p *Plugin) Search(ctx context.Context, req *pluginv1.SearchMetadataRequest) (*pluginv1.SearchMetadataResponse, error) {
	base, err := p.configured()
	if err != nil {
		return nil, err
	}
	typ := stremioType(req.GetItemType())
	if typ == "" {
		return &pluginv1.SearchMetadataResponse{}, nil
	}
	var m addonManifest
	if err = p.getJSON(ctx, base+"/manifest.json", &m); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := []*pluginv1.ProviderSearchResult{}
	for _, c := range m.Catalogs {
		if c.Type != typ || !searchable(c.Extra) {
			continue
		}
		endpoint := base + "/catalog/" + url.PathEscape(typ) + "/" + url.PathEscape(c.ID) + "/search=" + url.PathEscape(req.GetQuery()) + ".json"
		var env catalogEnvelope
		if err = p.getJSON(ctx, endpoint, &env); err != nil {
			p.logger.Warn("catalog search failed", "catalog", c.ID, "error", err)
			continue
		}
		for _, x := range env.Metas {
			if seen[x.ID] {
				continue
			}
			y := year(x)
			if req.GetYear() != 0 && y != 0 && abs(int(req.GetYear())-int(y)) > 1 {
				continue
			}
			seen[x.ID] = true
			ids, _ := structpb.NewStruct(idsFor(x.ID))
			out = append(out, &pluginv1.ProviderSearchResult{ProviderId: x.ID, Title: x.Name, Year: y, ImageUrl: x.Poster, Overview: x.Description, ProviderIds: ids})
		}
	}
	return &pluginv1.SearchMetadataResponse{Results: out}, nil
}
func searchable(xs []struct {
	Name       string
	IsRequired bool `json:"isRequired"`
}) bool {
	for _, x := range xs {
		if x.Name == "search" {
			return true
		}
	}
	return false
}
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
func stremioType(t string) string {
	switch strings.ToLower(t) {
	case "movie":
		return "movie"
	case "series", "show", "tv":
		return "series"
	}
	return ""
}

func (p *Plugin) GetMetadata(ctx context.Context, req *pluginv1.GetMetadataRequest) (*pluginv1.GetMetadataResponse, error) {
	base, err := p.configured()
	if err != nil {
		return nil, err
	}
	typ := stremioType(req.GetItemType())
	if typ == "" || req.GetProviderId() == "" {
		return &pluginv1.GetMetadataResponse{}, nil
	}
	endpoint := base + "/meta/" + url.PathEscape(typ) + "/" + url.PathEscape(req.GetProviderId()) + ".json"
	var env metaEnvelope
	if err = p.getJSON(ctx, endpoint, &env); err != nil {
		return nil, err
	}
	if env.Meta.ID == "" {
		return &pluginv1.GetMetadataResponse{}, nil
	}
	item := toItem(env.Meta, req.GetItemType())
	rt := p.rottenTomatoesRatings(ctx, env.Meta.Name, req.GetItemType(), year(env.Meta))
	values := item.GetRatings().AsMap()
	if rt.Critic != nil {
		values["rt_critic"] = *rt.Critic
	}
	if rt.Audience != nil {
		values["rt_audience"] = *rt.Audience
	}
	item.Ratings, _ = structpb.NewStruct(values)
	return &pluginv1.GetMetadataResponse{Item: item}, nil
}

func (p *Plugin) GetImages(ctx context.Context, req *pluginv1.GetImagesRequest) (*pluginv1.GetImagesResponse, error) {
	base, err := p.configured()
	if err != nil {
		return nil, err
	}
	typ := stremioType(req.GetItemType())
	if typ == "" || req.GetProviderId() == "" {
		return &pluginv1.GetImagesResponse{}, nil
	}
	endpoint := base + "/meta/" + url.PathEscape(typ) + "/" + url.PathEscape(req.GetProviderId()) + ".json"
	var env metaEnvelope
	if err := p.getJSON(ctx, endpoint, &env); err != nil {
		return nil, err
	}
	images := make([]*pluginv1.ImageRecord, 0, 3)
	add := func(kind, imageURL string) {
		if strings.TrimSpace(imageURL) != "" {
			images = append(images, &pluginv1.ImageRecord{Kind: kind, Url: imageURL, Language: req.GetLanguage()})
		}
	}
	add("poster", env.Meta.Poster)
	add("backdrop", env.Meta.Background)
	add("logo", env.Meta.Logo)
	return &pluginv1.GetImagesResponse{Images: images}, nil
}

func toItem(m stremioMeta, itemType string) *pluginv1.MetadataItem {
	ids, _ := structpb.NewStruct(idsFor(m.ID))
	ratings := map[string]any{}
	if f, err := strconv.ParseFloat(m.IMDbRating, 64); err == nil {
		ratings["imdb"] = f
	}
	rs, _ := structpb.NewStruct(ratings)
	release := dateOnly(m.Released)
	y := year(m)
	runtime := parseRuntime(m.Runtime)
	credits := append(people(m.Director, "director"), people(m.Writer, "writer")...)
	credits = append(credits, people(m.Cast, "actor")...)
	return &pluginv1.MetadataItem{ProviderId: m.ID, ItemType: itemType, Title: m.Name, Year: y, Overview: m.Description, Genres: m.Genres, ProviderIds: ids, Ratings: rs, Runtime: runtime, Countries: splitList(m.Country), OriginalLanguage: m.Language, PosterPath: m.Poster, BackdropPath: m.Background, LogoPath: m.Logo, People: credits, ReleaseDate: release, FirstAirDate: release}
}
func idsFor(id string) map[string]any {
	out := map[string]any{"aiometadata": id}
	low := strings.ToLower(id)
	if strings.HasPrefix(low, "tt") {
		out["imdb"] = id
	}
	for _, prefix := range []string{"tmdb:", "tvdb:", "mal:", "kitsu:", "anilist:"} {
		if strings.HasPrefix(low, prefix) {
			out[strings.TrimSuffix(prefix, ":")] = id[len(prefix):]
		}
	}
	return out
}
func year(m stremioMeta) int32 {
	for _, s := range []string{m.ReleaseInfo, m.Released} {
		for i := 0; i+4 <= len(s); i++ {
			if n, e := strconv.Atoi(s[i : i+4]); e == nil && n >= 1800 && n <= 2200 {
				return int32(n)
			}
		}
	}
	return 0
}
func dateOnly(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}
func parseRuntime(s string) int32 {
	fields := strings.Fields(strings.ToLower(s))
	if len(fields) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(fields[0])
	if strings.Contains(s, "hour") {
		n *= 60
	}
	return int32(n)
}
func splitList(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}
func people(v any, kind string) []*pluginv1.PersonRecord {
	var names []string
	switch x := v.(type) {
	case string:
		names = splitList(x)
	case []any:
		for _, v := range x {
			if s, ok := v.(string); ok {
				names = append(names, s)
			}
		}
	}
	out := make([]*pluginv1.PersonRecord, 0, len(names))
	for i, n := range names {
		out = append(out, &pluginv1.PersonRecord{Name: n, Kind: kind, SortOrder: int32(i)})
	}
	return out
}

var _ = path.Join
