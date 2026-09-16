# EFG
Technical Assignment: Player Presence Service

Go project using [Box](https://github.com/fulldump/box) for the API and
[goconfig](https://github.com/fulldump/goconfig) for configuration.

## Requirements

Go 1.25.6 or later, Make, and Docker with Docker Compose.

## Development

```sh
make run                         # Start MongoDB, then the server on :8080
make run ARGS='-addr :9090'       # Use a different address
make test                        # Run all tests
make build                       # Build Linux, Windows, and macOS binaries in bin/
```

The API exposes `GET /health`, which returns `{"status":"ok"}`.

```sh
curl http://localhost:8080/health
```

## Configuration

`config.json` sets the HTTP listen address (`addr`, default: `:8080`) and
the required MongoDB connection URI (`mongo_uri`). goconfig loads this file
automatically, if present.

Example `config.json` (loaded automatically if present):

```json
{
  "addr": ":8080",
  "mongo_uri": "mongodb://localhost:27017/efg"
}
```

## Help

```sh
make help
```
