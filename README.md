# CDNproject

This repo contains two QUIC-focused demos:

- `simplecdn/`: a small CDN and origin pair that serve a static website and a real sample HLS stream over HTTP/3.
- `test-conMigration/`: a standalone QUIC connection-migration demo.

## What is implemented

- HTTP/3 origin server using `quic-go/http3`
- HTTP/3 CDN that fetches from the origin over QUIC
- In-memory LRU caching for normal `GET`/`HEAD` responses
- Range request bypass, so partial content stays origin-backed
- Sample HLS playlist and MPEG-TS segments in `simplecdn/static/hls/`
- Separate QUIC connection-migration client/server example

## Run the CDN demo

Build the binaries:

```bash
cd simplecdn
make build
```

Run the origin server from the static asset directory:

```bash
cd simplecdn/static
../bin/server
```

Run the CDN in another terminal:

```bash
cd simplecdn
./bin/cdn -origin https://localhost:443 -addr :8443
```

Then open:

- Origin: `https://localhost/`
- CDN: `https://localhost:8443/`
- HLS playlist through the CDN: `https://localhost:8443/hls/stream.m3u8`

Expected behavior:

- The first CDN request for a normal asset returns `X-Cache: MISS`
- Repeating the same request returns `X-Cache: HIT`
- Requests with a `Range` header are forwarded and remain `X-Cache: MISS`

## Run the connection-migration demo

Build:

```bash
cd test-conMigration
make build
```

Run the server:

```bash
cd test-conMigration
./bin/server
```

Run the client:

```bash
cd test-conMigration
QUIC_HOST=::1 QUIC_PORT=4242 ./bin/client
```

To demonstrate migration, keep the client running and change the client's network path or IP while the QUIC session is still open. The server logs address changes without tearing down the connection.

## Tests

The automated tests cover:

- CDN cache miss-to-hit behavior
- Range request cache bypass
- HLS playlist handling
- Blocking access to origin key material

Run them with:

```bash
GOCACHE=/tmp/go-cache go test ./...
cd simplecdn && GOCACHE=/tmp/go-cache go test ./...
```
