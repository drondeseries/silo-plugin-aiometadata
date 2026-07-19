# Silo AIOMetadata Provider

Standalone Silo `metadata_provider.v1` plugin backed by an existing [AIOMetadata](https://github.com/cedya77/aiometadata) configuration.

## What it does

The plugin translates AIOMetadata Stremio API into Silo native metadata-provider protocol. It supports movie and series search, titles, descriptions, dates, runtime, genres, IMDb ratings, external IDs, credits, posters, backdrops, and logos. AIOMetadata poster, backdrop, and logo choices are exposed in Silo Edit Metadata image picker.

It does not host AIOMetadata. You need a working AIOMetadata instance and saved configuration first.

## Install in Silo

Add this URL to Silo plugin repository/catalog settings:

```text
https://raw.githubusercontent.com/drondeseries/silo-plugin-aiometadata/main/repository.json
```

Refresh the catalog, select **AIOMetadata Provider**, install it, and open its settings.

## Get the AIOMetadata addon URL

1. Open your AIOMetadata instance `/configure` page.
2. Choose the metadata, artwork, language, search, and catalog options you want.
3. Save the configuration.
4. Copy the generated Stremio addon URL, including its user/configuration path.

Both forms are accepted:

```text
https://metadata.example.com/stremio/USER/CONFIG
https://metadata.example.com/stremio/USER/CONFIG/manifest.json
```

Treat this URL as a secret: it may contain your saved user configuration.

## Configure the plugin

- **Addon URL**: required. Paste the complete generated URL, not just the hostname or `/configure` URL.
- **API credential**: optional. A token is sent as both `Authorization: Bearer` and `X-API-Key`; `username:password` uses HTTP Basic authentication.
- **Request timeout**: optional, defaults to 20 seconds. Increase it for slow cold starts.

Save the configuration. Enable AIOMetadata in each desired movie or series library and set its provider priority. Refresh metadata for existing items if desired; newly scanned items use it automatically when enabled.

## Verify it

Search for a well-known movie from Silo identify or metadata editor. Results should include AIOMetadata artwork. If there are none, open the configured URL with `/manifest.json` appended and confirm it returns JSON and advertises a movie or series catalog with the `search` extra.

Direct lookup uses:

```text
<addon-url>/meta/movie/<id>.json
<addon-url>/meta/series/<id>.json
```

Search is manifest-driven: every matching searchable catalog is queried, results are deduplicated by ID, individual catalog failures are tolerated, and a requested year uses a plus-or-minus one-year filter.

## Troubleshooting

- **Plugin is not configured**: save a valid HTTP or HTTPS Addon URL.
- **No search results**: enable search catalogs in AIOMetadata, save again, and update Silo if the URL changed.
- **401 or 403**: verify the private URL and any reverse-proxy token or Basic credential.
- **404**: copy the generated Stremio URL, not the `/configure` page.
- **Timeouts**: verify Silo can reach AIOMetadata and increase the timeout.
- **Missing fields/artwork**: results depend on AIOMetadata provider keys, language, and artwork preferences.

## Updates

Refresh the Silo plugin catalog when a new release is published, then use Silo plugin update flow. Releases contain static Linux amd64 and arm64 binaries plus `checksums.txt`.

## Build from source

Go 1.26 or newer is required.

```sh
go test ./...
go vet ./...
CGO_ENABLED=0 go build -trimpath -o plugin ./cmd
```

Install `plugin` with `manifest.json`.

## Security and privacy

The plugin only makes outbound GET requests to the configured addon origin. Credentials and the private addon URL are not logged. Metadata and image URLs returned by AIOMetadata are forwarded to Silo.
